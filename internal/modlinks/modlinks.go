package modlinks

import (
	"encoding/xml"
	"fmt"
	"net/http"
)

type modLinks struct {
	Manifests []Manifest `xml:"Manifest"`
}

type Manifest struct {
	Name         string
	Description  string
	Version      string
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
	return links.Manifests, nil
}
