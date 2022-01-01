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
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/dpinela/hkmod/internal/modlinks"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Printf("usage: %s {list}\n", os.Args[0])
		os.Exit(2)
	}
	subcmd := os.Args[1]
	var err error
	switch subcmd {
	case "list":
		err = list(os.Args[2:])
	case "download":
		err = download(os.Args[2:])
	default:
		err = fmt.Errorf("unknown subcommand: %q", subcmd)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func download(mods []string) error {
	installdir := os.Getenv("HK15PATH")
	if installdir == "" {
		return fmt.Errorf("HK15PATH not defined")
	}
	manifests, err := modlinks.Get()
	if err != nil {
		return err
	}
	downloads, err := modlinks.TransitiveClosure(manifests, mods)
	if err != nil {
		return err
	}
	for _, dl := range downloads {
		fmt.Println("Installing", dl.Name)
		fmt.Println("Downloading", dl.Link.URL)
		file, err := downloadLink(dl.Link)
		if err != nil {
			fmt.Println(err)
			continue
		}
		fmt.Println("Extracting", dl.Name)
		if err := extractMod(file, dl.Name, installdir); err != nil {
			fmt.Println(err)
		}
	}
	return nil
}

func downloadLink(link modlinks.Link) (*bytes.Reader, error) {
	wrap := func(err error) error { return fmt.Errorf("download %s: %w", link.URL, err) }
	expectedSHA, err := hex.DecodeString(link.SHA256)
	if err != nil {
		return nil, wrap(err)
	}
	resp, err := http.Get(link.URL)
	if err != nil {
		return nil, wrap(err)
	}
	defer resp.Body.Close()
	if !(resp.StatusCode >= 200 && resp.StatusCode < 300) {
		return nil, fmt.Errorf("download %s: response status was %d", link.URL, resp.StatusCode)
	}
	sha := sha256.New()
	buf, err := io.ReadAll(io.TeeReader(resp.Body, sha))
	if err != nil {
		return nil, wrap(err)
	}
	if !bytes.Equal(sha.Sum(make([]byte, 0, sha256.Size)), expectedSHA) {
		return nil, fmt.Errorf("download %s: sha256 does not match manifest", link.URL)
	}
	return bytes.NewReader(buf), nil
}

func extractMod(zipfile *bytes.Reader, name, installdir string) error {
	wrap := func(err error) error { return fmt.Errorf("extract mod %s: %w", name, err) }
	archive, err := zip.NewReader(zipfile, zipfile.Size())
	if err != nil {
		return wrap(err)
	}
	for _, file := range archive.File {
		// Prevent us from accidentally (or not so accidentally, in case of a malicious input)
		// from writing outside the destination directory.
		dest := filepath.Join(installdir, "Mods", name, filepath.Join(string(filepath.Separator), filepath.FromSlash(file.Name)))
		if err := writeZipFile(dest, file); err != nil {
			return wrap(err)
		}
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
