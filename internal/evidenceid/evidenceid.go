package evidenceid

import (
	"sort"

	"github.com/JeremyDev87/theseus/internal/canonical"
	"github.com/JeremyDev87/theseus/internal/model"
)

const Version = 1

func Finding(finding model.Finding) string {
	profiles := append([]string(nil), finding.Profiles...)
	sort.Strings(profiles)
	key, _ := canonical.Marshal(struct {
		Kind     string   `json:"kind"`
		Version  int      `json:"version"`
		Code     string   `json:"code"`
		Probe    string   `json:"probe"`
		Field    string   `json:"field"`
		Profiles []string `json:"profiles"`
		Locator  string   `json:"locator"`
	}{
		Kind: "finding", Version: Version, Code: finding.Code, Probe: finding.Probe,
		Field: finding.Field, Profiles: profiles, Locator: finding.Locator,
	})
	return "ths:v1:" + canonical.SHA256String(key)
}

func Incomplete(evidence model.IncompleteEvidence) string {
	key, _ := canonical.Marshal(struct {
		Kind    string `json:"kind"`
		Version int    `json:"version"`
		Code    string `json:"code"`
		Profile string `json:"profile"`
		Stage   string `json:"stage"`
		Probe   string `json:"probe"`
	}{
		Kind: "incomplete", Version: Version, Code: evidence.Code,
		Profile: evidence.Profile, Stage: evidence.Stage, Probe: evidence.Probe,
	})
	return "ths:v1:" + canonical.SHA256String(key)
}
