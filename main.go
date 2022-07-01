package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/dpinela/hkmod/internal/modlinks"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Printf("usage: %s list [-s search] [-d]\n", os.Args[0])
		fmt.Printf("       %s install [-e] modnames [...]\n", os.Args[0])
		os.Exit(2)
	}
	subcmd := os.Args[1]
	var err error
	switch subcmd {
	case "list":
		err = list(os.Args[2:])
	case "install":
		err = install(os.Args[2:])
	default:
		err = fmt.Errorf("unknown subcommand: %q", subcmd)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func install(args []string) error {
	installdir := os.Getenv("HK15PATH")
	if installdir == "" {
		return fmt.Errorf("HK15PATH not defined")
	}
	cachedir, err := os.UserCacheDir()
	if err != nil {
		return fmt.Errorf("cache directory not available: %w", err)
	}
	cachedir = filepath.Join(cachedir, "hkmod")

	var exactMatch bool
	flags := flag.NewFlagSet("install", flag.ExitOnError)
	flags.BoolVar(&exactMatch, "e", false, "Match mod names exactly (case-sensitive)")
	flags.Parse(args)
	mods := flags.Args()

	manifests, err := modlinks.Get()
	if err != nil {
		return err
	}
	resolvedMods := make([]string, 0, len(mods))
	if exactMatch {
		for _, m := range mods {
			if findExactMod(manifests, m) {
				resolvedMods = append(resolvedMods, m)
			} else {
				fmt.Printf("mod %q does not exist\n", m)
			}
		}
	} else {
		modPatterns := make([]*regexp.Regexp, len(mods))
		for i, m := range mods {
			modPatterns[i], err = regexp.Compile("(?i)" + regexp.QuoteMeta(m))
			if err != nil {
				return err
			}
		}
	
		for i, p := range modPatterns {
			ms := findMatchingMods(manifests, p)
			if len(ms) == 0 {
				fmt.Printf("%q matches no mods\n", mods[i])
				continue
			}
			if len(ms) > 1 {
				fmt.Printf("%q is ambiguous: matches %s\n", mods[i], strings.Join(ms, ", "))
				continue
			}
			resolvedMods = append(resolvedMods, ms[0])
		}
	}
	
	downloads, err := modlinks.TransitiveClosure(manifests, resolvedMods)
	if err != nil {
		return err
	}
	for _, dl := range downloads {
		// There's no way we can reasonably install a mod whose name contains a path separator.
		// This also avoids any path traversal vulnerabilities from mod names.
		if strings.ContainsRune(dl.Name, filepath.Separator) {
			fmt.Printf("cannot install %s: contains path separator\n", dl.Name)
			continue
		}
		if strings.ContainsRune(path.Base(dl.Link.URL), filepath.Separator) {
			fmt.Printf("cannot install %s: filename contains path separator\n", dl.Name)
			continue
		}
		fmt.Println("Installing", dl.Name)
		file, size, err := getModFile(cachedir, &dl)
		if err != nil {
			fmt.Println(err)
			continue
		}
		fmt.Println("Extracting", dl.Name)
		if err := removePreviousVersion(dl.Name, installdir); err != nil {
			fmt.Println(err)
			continue
		}
		if path.Ext(dl.Link.URL) == ".zip" {
			err = extractModZip(file, size, dl.Name, installdir)
		} else {
			err = extractModDLL(file, path.Base(dl.Link.URL), dl.Name, installdir)
		}
		if err != nil {
			fmt.Println(err)
		}
	}
	return nil
}

func findMatchingMods(ms []modlinks.Manifest, p *regexp.Regexp) []string {
	var matched []string
	for _, m := range ms {
		if p.MatchString(m.Name) {
			matched = append(matched, m.Name)
		}
	}
	return matched
}

func findExactMod(ms []modlinks.Manifest, name string) bool {
	for _, m := range ms {
		if m.Name == name {
			return true
		}
	}
	return false
}

type readCloserAt interface {
	io.ReaderAt
	io.ReadSeekCloser
}

func getModFile(cachedir string, mod *modlinks.Manifest) (readCloserAt, int64, error) {
	expectedSHA, err := hex.DecodeString(mod.Link.SHA256)
	if err != nil {
		return nil, 0, err
	}
	cacheEntry := filepath.Join(cachedir, mod.Name + path.Ext(mod.Link.URL))
	f, err := os.Open(cacheEntry)
	if os.IsNotExist(err) {
		return downloadLink(cacheEntry, mod.Link.URL, expectedSHA)
	}
	if err != nil {
		return nil, 0, err
	}
	sha := sha256.New()
	size, err := io.Copy(sha, f)
	if err != nil {
		f.Close()
		return nil, 0, err
	}
	if !bytes.Equal(expectedSHA, sha.Sum(make([]byte, 0, sha256.Size))) {
		f.Close()
		return downloadLink(cacheEntry, mod.Link.URL, expectedSHA)
	}
	fmt.Println("Got", mod.Name, "from cache")
	return f, size, nil
}

func downloadLink(localfile string, url string, expectedSHA []byte) (readCloserAt, int64, error) {
	fmt.Println("Downloading", url)
	wrap := func(err error) error { return fmt.Errorf("download %s: %w", url, err) }
	resp, err := http.Get(url)
	if err != nil {
		return nil, 0, wrap(err)
	}
	defer resp.Body.Close()
	if !(resp.StatusCode >= 200 && resp.StatusCode < 300) {
		return nil, 0, fmt.Errorf("download %s: response status was %d", url, resp.StatusCode)
	}
	if err := os.MkdirAll(filepath.Dir(localfile), 0750); err != nil {
		return nil, 0, wrap(err)
	}
	f, err := os.Create(localfile)
	if err != nil {
		return nil, 0, wrap(err)
	}
	sha := sha256.New()
	size, err := io.Copy(f, io.TeeReader(resp.Body, sha))
	if err != nil {
		f.Close()
		return nil, 0, wrap(err)
	}
	if !bytes.Equal(sha.Sum(make([]byte, 0, sha256.Size)), expectedSHA) {
		return nil, 0, fmt.Errorf("download %s: sha256 does not match manifest", url)
	}
	return f, size, nil
}

func removePreviousVersion(name, installdir string) error {
	// Keep existing skins while reinstalling Custom Knight.
	if name == "Custom Knight" {
		return removePreviousDLLs(name, installdir)
	}
	err := os.RemoveAll(filepath.Join(installdir, "Mods", name))
	if err == nil || os.IsNotExist(err) {
		return nil
	}
	return fmt.Errorf("remove previous version of %s: %w", name, err)
}

func removePreviousDLLs(name, installdir string) error {
	moddir := filepath.Join(installdir, "Mods", name)
	dir, err := os.Open(moddir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer dir.Close()
	entries, err := dir.ReadDir(-1)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".dll" {
			if err := os.Remove(filepath.Join(moddir, e.Name())); err != nil {
				fmt.Println("warning:", err)
			}
		}
	}
	return nil
}

func extractModZip(zipfile io.ReaderAt, size int64, name, installdir string) error {
	wrap := func(err error) error { return fmt.Errorf("extract mod %s: %w", name, err) }
	archive, err := zip.NewReader(zipfile, size)
	if err != nil {
		return wrap(err)
	}
	for _, file := range archive.File {
		// Prevent us from accidentally (or not so accidentally, in case of a malicious input)
		// from writing outside the destination directory.
		dest := filepath.Join(installdir, "Mods", name, filepath.Join(string(filepath.Separator), filepath.FromSlash(file.Name)))
		if strings.HasSuffix(file.Name, "/") {
			err = os.MkdirAll(dest, 0750)
		} else {
			err = writeZipFile(dest, file)
		}
		if err != nil {
			return wrap(err)
		}
	}
	return nil
}

func extractModDLL(dllfile io.ReadSeeker, filename, modname, installdir string) error {
	wrap := func(err error) error { return fmt.Errorf("extract mod %s: %w", modname, err) }
	dest := filepath.Join(installdir, "Mods", modname, filename)
	if err := os.MkdirAll(filepath.Dir(dest), 0750); err != nil {
		return wrap(err)
	}
	if _, err := dllfile.Seek(0, io.SeekStart); err != nil {
		return wrap(err)
	}
	w, err := os.Create(dest)
	if err != nil {
		return wrap(err)
	}
	_, err = io.Copy(w, dllfile)
	if err != nil {
		w.Close()
		return wrap(err)
	}
	if err := w.Close(); err != nil {
		return wrap(err)
	}
	return nil
}

func writeZipFile(dest string, file *zip.File) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0750); err != nil {
		return err
	}
	w, err := os.Create(dest)
	if err != nil {
		return err
	}
	r, err := file.Open()
	if err != nil {
		w.Close()
		return err
	}
	_, err = io.Copy(w, r)
	if err != nil {
		r.Close()
		w.Close()
		return err
	}
	if err := r.Close(); err != nil {
		w.Close()
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	if err := os.Chtimes(dest, file.Modified, file.Modified); err != nil {
		fmt.Println("warning:", err)
	}
	return nil
}

func list(args []string) error {
	flags := flag.NewFlagSet("list", flag.ExitOnError)
	var detailed bool
	var search string
	flags.BoolVar(&detailed, "d", false, "Display detailed information about mods")
	flags.StringVar(&search, "s", "", "Search for mods whose name contains `term`")
	if err := flags.Parse(args); err != nil {
		return err
	}
	manifests, err := modlinks.Get()
	if err != nil {
		return err
	}
	if search != "" {
		pattern, err := regexp.Compile("(?i)" + regexp.QuoteMeta(search))
		if err != nil {
			return err
		}
		filtered := manifests[:0]
		for _, m := range manifests {
			if pattern.MatchString(m.Name) {
				filtered = append(filtered, m)
			}
		}
		manifests = filtered
	}
	sort.Slice(manifests, func(i, j int) bool { return manifests[i].Name < manifests[j].Name })
	for _, m := range manifests {
		fmt.Println(m.Name)
		if detailed {
			fmt.Println("\tVersion:", m.Version)
			fmt.Println("\tRepository:", m.Repository)
			deps := "none"
			if len(m.Dependencies) > 0 {
				deps = strings.Join(m.Dependencies, ", ")
			}
			fmt.Println("\tDependencies:", deps)
			fmt.Printf("\t%s\n\n", strings.ReplaceAll(m.Description, "\n", "\n\t"))
		}
	}
	return nil
}
