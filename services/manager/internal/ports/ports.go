package ports

import (
	"fmt"
	"net"
	"strconv"
)

// Offsets inside a port block. They mirror the Supabase CLI defaults
// (54321 api, 54322 db, ...) so block 0 at base 54300 matches a stock project.
const (
	OffsetShadow    = 20
	OffsetAPI       = 21
	OffsetDB        = 22
	OffsetStudio    = 23
	OffsetSMTP      = 24
	OffsetAnalytics = 27
	OffsetPooler    = 29
)

type Ports struct {
	API       int `json:"api"`
	DB        int `json:"db"`
	Shadow    int `json:"shadow"`
	Pooler    int `json:"pooler"`
	Studio    int `json:"studio"`
	SMTP      int `json:"smtp"`
	Analytics int `json:"analytics"`
	Inspector int `json:"inspector"`
}

func ForBase(base, inspector int) Ports {
	return Ports{
		API:       base + OffsetAPI,
		DB:        base + OffsetDB,
		Shadow:    base + OffsetShadow,
		Pooler:    base + OffsetPooler,
		Studio:    base + OffsetStudio,
		SMTP:      base + OffsetSMTP,
		Analytics: base + OffsetAnalytics,
		Inspector: inspector,
	}
}

func (p Ports) List() []int {
	return []int{p.API, p.DB, p.Shadow, p.Pooler, p.Studio, p.SMTP, p.Analytics, p.Inspector}
}

type Allocator struct {
	Start          int
	Block          int
	InspectorStart int
	MaxBlocks      int
}

func NewAllocator(start, block, inspectorStart int) *Allocator {
	return &Allocator{Start: start, Block: block, InspectorStart: inspectorStart, MaxBlocks: 100}
}

// Next returns the first block whose base and inspector port are not taken in the
// database and whose ports are all free on the host.
func (a *Allocator) Next(usedBases, usedInspectors map[int]bool) (base, inspector int, err error) {
	for n := 0; n < a.MaxBlocks; n++ {
		base = a.Start + n*a.Block
		if usedBases[base] {
			continue
		}
		inspector = a.nextInspector(usedInspectors)
		if inspector == 0 {
			break
		}
		if AllFree(ForBase(base, inspector).List()) {
			return base, inspector, nil
		}
	}
	return 0, 0, fmt.Errorf("no free port block available")
}

func (a *Allocator) nextInspector(used map[int]bool) int {
	for p := a.InspectorStart; p < a.InspectorStart+a.MaxBlocks; p++ {
		if !used[p] && IsFree(p) {
			return p
		}
	}
	return 0
}

func IsFree(port int) bool {
	l, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return false
	}
	_ = l.Close()
	return true
}

func AllFree(ports []int) bool {
	for _, p := range ports {
		if !IsFree(p) {
			return false
		}
	}
	return true
}
