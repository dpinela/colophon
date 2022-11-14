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
		fmt.Printf("       %s install modnames [...]\n", os.Args[0])
		fmt.Printf("       %s installfile modname path-or-url", os.Args[0])
		fmt.Printf("       %s yeet modnames [...]\n", os.Args[0])
		fmt.Printf("       %s publish -url modfileurl -modlinks repopath [-name modname] [-version number] [-desc text] [-deps dep1,dep2,...] [-repo url]\n", os.Args[0])
		os.Exit(2)
	}
	subcmd := os.Args[1]
	var err error
	switch subcmd {
	case "list":
		err = list(os.Args[2:])
	case "install":
		err = install(os.Args[2:])
	case "installfile":
		err = installfile(os.Args[2:])
	case "yeet":
		err = yeet(os.Args[2:])
	case "publish":
		err = publish(os.Args[2:])
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

	manifests, err := modlinks.Get()
	if err != nil {
		return err
	}
	resolvedMods := make([]string, 0, len(args))
	for _, requestedName := range args {
		mod, err := resolveMod(manifests, requestedName)
		if err != nil {
			fmt.Println(err)
			continue
		}
		resolvedMods = append(resolvedMods, mod)
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
			file.Close()
			continue
		}
		if path.Ext(dl.Link.URL) == ".zip" {
			err = extractModZip(file, size, dl.Name, installdir)
		} else {
			err = extractModDLL(file, path.Base(dl.Link.URL), dl.Name, installdir)
		}
		file.Close()
		if err != nil {
			fmt.Println(err)
		}
	}
	return nil
}

func installfile(args []string) error {
	installdir := os.Getenv("HK15PATH")
	if installdir == "" {
		return fmt.Errorf("HK15PATH not defined")
	}
	if len(args) < 2 {
		return fmt.Errorf("usage: installfile modname path-or-url")
	}
	name := args[0]
	source := args[1]
	var file interface {
		io.ReadSeeker
		io.ReaderAt
	}
	var size int64
	if regexp.MustCompile("^https?://").MatchString(source) {
		resp, err := http.Get(source)
		if err != nil {
			return fmt.Errorf("download %s: %w", source, err)
		}
		content, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return fmt.Errorf("download %s: %w", source, err)
		}
		size = int64(len(content))
		file = bytes.NewReader(content)
	} else {
		f, err := os.Open(source)
		if err != nil {
			return err
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil {
			return err
		}
		size = info.Size()
		file = f
	}
	if path.Ext(source) == ".zip" {
		return extractModZip(file, size, name, installdir)
	} else {
		return extractModDLL(file, path.Base(source), name, installdir)
	}
}

type unknownModError struct{ requestedName string }

func (err *unknownModError) Error() string {
	return fmt.Sprintf("%q matches no mods", err.requestedName)
}

type ambiguousModError struct {
	requestedName string
	possibilities []string
}

func (err *ambiguousModError) Error() string {
	return fmt.Sprintf("%q is ambiguous: matches %s", err.requestedName, strings.Join(err.possibilities, ", "))
}

type duplicateModError struct {
	requestedName string
	numMatches    int
}

func (err *duplicateModError) Error() string {
	return fmt.Sprintf("%q is ambiguous: %d mods with that exact name exist", err.requestedName, err.numMatches)
}

func resolveMod(ms []modlinks.Manifest, requestedName string) (string, error) {
	names := make([]string, len(ms))
	for i, m := range ms {
		names[i] = m.Name
	}
	return resolveModName(names, requestedName)
}

func resolveModName(ms []string, requestedName string) (string, error) {
	pattern, err := regexp.Compile("(?i)" + regexp.QuoteMeta(requestedName))
	if err != nil {
		return "", err
	}
	var matches []string
	for _, m := range ms {
		if pattern.MatchString(m) {
			matches = append(matches, m)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return "", &unknownModError{requestedName}
	}

	fullMatches := matches[:0]
	for _, m := range matches {
		if strings.EqualFold(m, requestedName) {
			fullMatches = append(fullMatches, m)
		}
	}
	switch len(fullMatches) {
	case 1:
		return fullMatches[0], nil
	case 0:
		// If fullMatches is empty, the previous loop never appended to it, so the contents of
		// the matches slice are intact.
		return "", &ambiguousModError{requestedName, matches}
	}

	numExactMatches := 0
	for _, m := range fullMatches {
		if m == requestedName {
			numExactMatches++
		}
	}
	switch numExactMatches {
	case 1:
		return requestedName, nil
	case 0:
		return "", &ambiguousModError{requestedName, fullMatches}
	default:
		return "", &duplicateModError{requestedName, numExactMatches}
	}
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
	cacheEntry := filepath.Join(cachedir, mod.Name+path.Ext(mod.Link.URL))
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
	if !isHTTPOK(resp.StatusCode) {
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

func isHTTPOK(code int) bool { return code >= 200 && code < 300 }

const customKnightName = "Custom Knight"

func removePreviousVersion(name, installdir string) error {
	// Keep existing skins while reinstalling Custom Knight.
	if name == customKnightName {
		return removePreviousDLLs(name, installdir)
	}
	err := os.RemoveAll(filepath.Join(installdir, "Mods", name))
	if err == nil || os.IsNotExist(err) {
		return nil
	}
	return fmt.Errorf("yeet installed version of %s: %w", name, err)
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

func yeet(args []string) error {
	installdir := os.Getenv("HK15PATH")
	if installdir == "" {
		return fmt.Errorf("HK15PATH not defined")
	}
	modsdir := filepath.Join(installdir, "Mods")
	mods, err := installedMods(modsdir)
	if err != nil {
		return err
	}
	modsToDelete := map[string]struct{}{}
	for _, arg := range args {
		resolved, err := resolveModName(mods, arg)
		if err != nil {
			fmt.Println(err)
			continue
		}
		modsToDelete[resolved] = struct{}{}
	}
	for mod := range modsToDelete {
		if err := removePreviousVersion(mod, installdir); err != nil {
			fmt.Println(err)
		} else if mod == customKnightName {
			fmt.Println("Yeeted", mod, "(installed skins kept)")
		} else {
			fmt.Println("Yeeted", mod)
		}
	}
	return nil
}

func installedMods(modsdir string) ([]string, error) {
	wrap := func(err error) error {
		return fmt.Errorf("list installed mods: %w", err)
	}

	dir, err := os.Open(modsdir)
	if err != nil {
		return nil, wrap(err)
	}
	entries, err := dir.ReadDir(0)
	dir.Close()
	if err != nil {
		return nil, wrap(err)
	}
	// We expect almost all of the entries in the Mods directory to be actual mods.
	modnames := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() && !strings.EqualFold(strings.TrimSpace(e.Name()), "Disabled") {
			modnames = append(modnames, e.Name())
		}
	}
	return modnames, nil
}

func publish(args []string) error {
	var manifestPatch modlinks.Manifest
	var modlinksPath, deps string

	flags := flag.NewFlagSet("publish", flag.ExitOnError)
	flags.StringVar(&manifestPatch.Link.URL, "url", "", "The mod file that will be published on modlinks (required)")
	flags.StringVar(&modlinksPath, "modlinks", "ModLinks.xml", "Path to the modlinks file")
	flags.StringVar(&manifestPatch.Name, "name", "", "The name of the mod (will be determined from the URL if not specified)")
	flags.StringVar(&manifestPatch.Version, "version", "", "The version of the mod (will be determined from the URL if not specified)")
	flags.StringVar(&manifestPatch.Description, "desc", "", "The description")
	flags.StringVar(&deps, "deps", "", "The mod's dependencies, separated by commas ('none' to remove all dependencies when updating)")
	flags.StringVar(&manifestPatch.Repository, "repo", "", "The URL for the mod's repository")
	flags.Parse(args)

	if manifestPatch.Link.URL == "" {
		return fmt.Errorf("publish %q: no mod file URL specified", manifestPatch.Name)
	}
	if manifestPatch.Name == "" {
		url := manifestPatch.Link.URL
		manifestPatch.Name = strings.TrimSuffix(path.Base(url), path.Ext(url))
	}
	if manifestPatch.Name == "" {
		return fmt.Errorf("publish %q: name could not be determined from URL", manifestPatch.Link.URL)
	}
	if manifestPatch.Version == "" {
		m := regexp.MustCompile(`/v(\d+(?:\.\d+)*)/`).FindStringSubmatch(manifestPatch.Link.URL)
		if m == nil {
			return fmt.Errorf("publish %q: version could not be determined from URL", manifestPatch.Name)
		}
		manifestPatch.Version = m[1]
	}
	manifestPatch.Version = padVersion(manifestPatch.Version)
	switch deps {
	case "none":
		manifestPatch.Dependencies = make([]string, 0)
	case "":
		manifestPatch.Dependencies = nil
	default:
		manifestPatch.Dependencies = strings.Split(deps, ",")
	}

	wrap := func(err error) error {
		return fmt.Errorf("publish %q: %w", manifestPatch.Name, err)
	}

	sha, err := sha256OfURL(manifestPatch.Link.URL)
	if err != nil {
		return wrap(err)
	}
	manifestPatch.Link.SHA256 = sha

	modlinksFile, err := os.OpenFile(modlinksPath, os.O_RDWR, 0)
	if err != nil {
		return wrap(err)
	}
	defer modlinksFile.Close()
	content, err := io.ReadAll(modlinksFile)
	if err != nil {
		return err
	}
	blocks := regexp.MustCompile(`(?s)<Manifest>.*?</Manifest>`).FindAllIndex(content, -1)
	namePattern := []byte(`<Name>` + manifestPatch.Name + `</Name>`)
	for _, bounds := range blocks {
		blockContent := content[bounds[0]:bounds[1]]
		if bytes.Contains(blockContent, namePattern) {
			m, err := modlinks.ParseManifest(blockContent)
			if err != nil {
				return err
			}
			m.Merge(manifestPatch)
			newContent := bytes.TrimLeft(modlinks.EncodeManifest(m), " ")

			if err := mustSeek(modlinksFile, int64(bounds[0])); err != nil {
				return err
			}
			if err := modlinksFile.Truncate(int64(bounds[0]) + int64(len(newContent)) + int64(len(content)-bounds[1])); err != nil {
				return err
			}
			if _, err := modlinksFile.Write(newContent); err != nil {
				return err
			}
			if _, err := modlinksFile.Write(content[bounds[1]:]); err != nil {
				return err
			}
			return nil
		}
	}
	if len(blocks) == 0 {
		return fmt.Errorf("publish %q: cannot find insertion point for new manifest", manifestPatch.Name)
	}
	newContent := []byte{'\n'}
	newContent = append(newContent, modlinks.EncodeManifest(manifestPatch)...)
	endOfLastManifest := blocks[len(blocks)-1][1]
	if err := mustSeek(modlinksFile, int64(endOfLastManifest)); err != nil {
		return err
	}
	if _, err := modlinksFile.Write(newContent); err != nil {
		return err
	}
	if _, err := modlinksFile.Write(content[endOfLastManifest:]); err != nil {
		return err
	}
	return nil
}

func mustSeek(f io.Seeker, target int64) error {
	off, err := f.Seek(target, io.SeekStart)
	if err != nil {
		return err
	}
	if off != target {
		return fmt.Errorf("seek should have gone to offset %d, went to %d instead", target, off)
	}
	return nil
}

func padVersion(v string) string {
	nums := strings.Split(v, ".")
	for len(nums) < 4 {
		nums = append(nums, "0")
	}
	return strings.Join(nums, ".")
}

func sha256OfURL(link string) (string, error) {
	resp, err := http.Get(link)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if !isHTTPOK(resp.StatusCode) {
		return "", fmt.Errorf("download %s: response status was %d", link, resp.StatusCode)
	}
	sha := sha256.New()
	if _, err := io.Copy(sha, resp.Body); err != nil {
		return "", err
	}
	result := make([]byte, 0, sha256.Size)
	return hex.EncodeToString(sha.Sum(result)), nil
}
