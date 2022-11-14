package modlinks

import (
	"encoding/xml"
	"fmt"
	"net/http"
	"strings"
)

type modLinks struct {
	Manifests []Manifest `xml:"Manifest"`
}

type Manifest struct {
	Name         string
	Description  string
	Version      string
	Repository   string
	Link         Link
	Dependencies []string `xml:"Dependencies>Dependency"`
}

type Link struct {
	SHA256 string `xml:",attr"`
	URL    string `xml:",chardata"`
}

const modlinksURL = "https://raw.githubusercontent.com/hk-modding/modlinks/main/ModLinks.xml"

func Get() ([]Manifest, error) {
	wrap := func(err error) error { return fmt.Errorf("get modlinks: %w", err) }
	resp, err := http.Get(modlinksURL)
	if err != nil {
		return nil, wrap(err)
	}
	defer resp.Body.Close()
	if !(resp.StatusCode >= 200 && resp.StatusCode < 300) {
		return nil, fmt.Errorf("get modlinks: response status was %d", resp.StatusCode)
	}
	var links modLinks
	if err := xml.NewDecoder(resp.Body).Decode(&links); err != nil {
		return nil, wrap(err)
	}
	// The Link and Repository fields have some extra indentation inside them; discard it.
	for i := range links.Manifests {
		m := &links.Manifests[i]
		m.Link.URL = strings.TrimSpace(m.Link.URL)
		m.Repository = strings.TrimSpace(m.Repository)
	}
	return links.Manifests, nil
}

func ParseManifest(text []byte) (Manifest, error) {
	var m Manifest
	err := xml.Unmarshal(text, &m)
	return m, err
}

func EncodeManifest(m Manifest) []byte {
	text, err := xml.MarshalIndent(m, "    ", "    ")
	if err != nil {
		panic(err)
	}
	return text
}

type missingModsError []string

func (err missingModsError) Error() string {
	return fmt.Sprintf("required mods do not exist: %s", strings.Join(err, ","))
}

func TransitiveClosure(allModlinks []Manifest, mods []string) ([]Manifest, error) {
	modsByName := make(map[string]*Manifest, len(allModlinks))
	for i := range allModlinks {
		modsByName[allModlinks[i].Name] = &allModlinks[i]
	}
	resultSet := map[string]*Manifest{}
	missingModSet := map[string]bool{}
	for _, name := range mods {
		transitiveClosure(modsByName, resultSet, missingModSet, name)
	}
	result := make([]Manifest, 0, len(resultSet))
	for _, mod := range resultSet {
		result = append(result, *mod)
	}
	missing := make(missingModsError, 0, len(missingModSet))
	for name := range missingModSet {
		missing = append(missing, name)
	}
	var err error
	if len(missing) > 0 {
		err = missing
	}
	return result, err
}

func transitiveClosure(modsByName, resultSet map[string]*Manifest, missingMods map[string]bool, modName string) {
	if _, ok := resultSet[modName]; ok {
		return
	}
	mod, ok := modsByName[modName]
	if !ok {
		missingMods[modName] = true
		return
	}
	resultSet[modName] = mod
	for _, dep := range mod.Dependencies {
		transitiveClosure(modsByName, resultSet, missingMods, dep)
	}
}
