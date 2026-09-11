package playergen

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
)

// On-disk curated-data schema. One file per nationality:
// data/names/<code>.json. The curated files are the versioned,
// license-attributed record of truth for the global player pool.
type NationFile struct {
	Nationality FileNationality `json:"nationality"`
	Provenance  FileProvenance  `json:"provenance"`
	FirstNames  []string        `json:"first_names"`
	LastNames   []string        `json:"last_names"`
}

type FileNationality struct {
	Code             string  `json:"code"`
	Name             string  `json:"name"`
	GenerationWeight float64 `json:"generation_weight"`
}

type FileProvenance struct {
	Source    string `json:"source"`
	License   string `json:"license"`
	CuratedBy string `json:"curated_by"`
	Date      string `json:"date"`
	Verified  bool   `json:"verified"`
}

// codeFilePattern matches the only files LoadNameData will read. It deliberately
// excludes documentation/example files such as nigeria.example.json.
var codeFilePattern = regexp.MustCompile(`^[a-z]{2,3}\.json$`)

// NameData is everything loaded from the curated files: the weighted
// nationality pool and the in-memory name generator. It contains no database
// dependency; the ref-seeder walks the same files to populate ref.nationalities
// and ref.name_pool.
type NameData struct {
	Generator     *PoolGenerator
	Pool          *NationalityPool
	Nationalities []Nationality
}

// LoadNameData reads every data/names/<code>.json file under dataDir. A
// malformed file, an invalid code, or an empty name list is an error — the
// previous silent "Unknown" fallback is removed by design, so missing coverage
// fails loudly at load time.
func LoadNameData(dataDir string) (*NameData, error) {
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		return nil, fmt.Errorf("playergen: read name data dir %q: %w", dataDir, err)
	}

	data := &NameData{
		Generator: NewPoolGenerator(),
		Pool:      NewNationalityPool(),
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if codeFilePattern.MatchString(e.Name()) {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)
	if len(files) == 0 {
		return nil, fmt.Errorf("playergen: no curated name files matching <code>.json in %q", dataDir)
	}

	for _, name := range files {
		path := filepath.Join(dataDir, name)
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("playergen: read %q: %w", name, err)
		}
		var nf NationFile
		if err := json.Unmarshal(raw, &nf); err != nil {
			return nil, fmt.Errorf("playergen: parse %q: %w", name, err)
		}
		if err := validateNationFile(name, &nf); err != nil {
			return nil, err
		}
		if err := data.Generator.AddPool(nf.Nationality.Code, nf.FirstNames, nf.LastNames); err != nil {
			return nil, err
		}
		nationality := Nationality{
			Code:   nf.Nationality.Code,
			Name:   nf.Nationality.Name,
			Weight: nf.Nationality.GenerationWeight,
		}
		data.Pool.Add(nationality)
		data.Nationalities = append(data.Nationalities, nationality)
	}

	return data, nil
}

func validateNationFile(fileName string, nf *NationFile) error {
	if !regexp.MustCompile(`^[a-z]{2,3}$`).MatchString(nf.Nationality.Code) {
		return fmt.Errorf("playergen: %q: invalid nationality code %q (expected lowercase 2-3 letter slug)", fileName, nf.Nationality.Code)
	}
	if nf.Nationality.Name == "" {
		return fmt.Errorf("playergen: %q: nationality.name is required", fileName)
	}
	if nf.Nationality.GenerationWeight <= 0 {
		return fmt.Errorf("playergen: %q: generation_weight must be > 0", fileName)
	}
	if len(nf.FirstNames) == 0 {
		return fmt.Errorf("playergen: %q: first_names is empty", fileName)
	}
	if len(nf.LastNames) == 0 {
		return fmt.Errorf("playergen: %q: last_names is empty", fileName)
	}
	if nf.Provenance.Source == "" || nf.Provenance.License == "" {
		return fmt.Errorf("playergen: %q: provenance.source and provenance.license are required", fileName)
	}
	return nil
}
