package playergen

import (
	"math/rand"
	"os"
	"encoding/json"
	"fmt"
	"sync"
)

type namePool struct {
	FirstNames []string `json:"first_names"`
	LastNames  []string `json:"last_names"`
}

// FileBackedGenerator reads name lists from JSON files in data/names/.
type FileBackedGenerator struct {
	mu    sync.RWMutex
	pools map[string]*namePool
}

func NewFileBackedGenerator(dataDir string) (*FileBackedGenerator, error) {
	g := &FileBackedGenerator{
		pools: make(map[string]*namePool),
	}

	// Load nationality pool
	nationalities := []string{
		"nigeria", "brazil", "argentina", "france", "england",
		"spain", "germany", "italy", "portugal", "netherlands",
		"belgium", "japan", "south_korea", "usa", "mexico",
	}

	for _, nat := range nationalities {
		path := fmt.Sprintf("%s/%s.json", dataDir, nat)
		data, err := os.ReadFile(path)
		if err != nil {
			continue // skip missing files, not fatal at load time
		}
		var pool namePool
		if err := json.Unmarshal(data, &pool); err != nil {
			continue
		}
		g.mu.Lock()
		g.pools[nat] = &pool
		g.mu.Unlock()
	}

	return g, nil
}

func (g *FileBackedGenerator) GenerateFirstName(nationality string) string {
	g.mu.RLock()
	pool, ok := g.pools[nationality]
	g.mu.RUnlock()
	if !ok || len(pool.FirstNames) == 0 {
		return "Unknown"
	}
	return pool.FirstNames[rand.Intn(len(pool.FirstNames))]
}

func (g *FileBackedGenerator) GenerateLastName(nationality string) string {
	g.mu.RLock()
	pool, ok := g.pools[nationality]
	g.mu.RUnlock()
	if !ok || len(pool.LastNames) == 0 {
		return "Unknown"
	}
	return pool.LastNames[rand.Intn(len(pool.LastNames))]
}

func (g *FileBackedGenerator) GenerateFullName(nationality string) (string, string) {
	return g.GenerateFirstName(nationality), g.GenerateLastName(nationality)
}
