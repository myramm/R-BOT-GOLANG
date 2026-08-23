package errortracker

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"rbot/brain/store"
)

type ProposedFix struct {
	Summary           string   `json:"summary"`
	Steps             []string `json:"steps"`
	AffectedFiles     []string `json:"affectedFiles"`
	AffectedFunctions []string `json:"affectedFunctions"`
	RiskAssessment    string   `json:"riskAssessment"`
	RecommendedTests  []string `json:"recommendedTests"`
}

type OwnerDecision struct {
	Approved  bool   `json:"approved"`
	DecidedAt string `json:"decidedAt"`
	Note      string `json:"note,omitempty"`
}

type FixResult struct {
	Success     bool     `json:"success"`
	CompletedAt string   `json:"completedAt"`
	TestsPassed []string `json:"testsPassed,omitempty"`
	FailedTests []string `json:"failedTests,omitempty"`
	DiffSummary string   `json:"diffSummary,omitempty"`
	OutputLog   string   `json:"outputLog,omitempty"`
}

type BugReport struct {
	ID                string         `json:"id"`
	Fingerprint       string         `json:"fingerprint"`
	Service           string         `json:"service"`
	Error             string         `json:"error"`
	Stack             string         `json:"stack,omitempty"`
	Severity          string         `json:"severity"` // "LOW" | "MEDIUM" | "HIGH" | "CRITICAL"
	RootCause         string         `json:"rootCause"`
	AffectedFiles     []string       `json:"affectedFiles"`
	AffectedFunctions []string       `json:"affectedFunctions"`
	ProposedFix       ProposedFix    `json:"proposedFix"`
	Status            string         `json:"status"` // "WAITING_FOR_OWNER_APPROVAL" | "APPROVED" | "REJECTED" | "FIXING" | "TESTING" | "FIXED" | "FAILED"
	IncidentCount     int            `json:"incidentCount"`
	FirstSeen         string         `json:"firstSeen"`
	LastSeen          string         `json:"lastSeen"`
	OwnerDecision     *OwnerDecision `json:"ownerDecision,omitempty"`
	FixBranch         string         `json:"fixBranch,omitempty"`
	FixResult         *FixResult     `json:"fixResult,omitempty"`
	CreatedAt         string         `json:"createdAt"`
	UpdatedAt         string         `json:"updatedAt"`
}

type BugStore struct {
	mu   sync.RWMutex
	bugs []BugReport
}

var DefaultBugStore = &BugStore{
	bugs: make([]BugReport, 0),
}

func (s *BugStore) Load() {
	s.mu.Lock()
	defer s.mu.Unlock()
	var saved []BugReport
	found, err := store.Get("agy_bugs_registry", &saved)
	if err == nil && found && saved != nil {
		s.bugs = saved
	}
}

func (s *BugStore) save() {
	_ = store.Set("agy_bugs_registry", s.bugs)
}

func GetBugs(status string) []BugReport {
	DefaultBugStore.mu.RLock()
	DefaultBugStore.Load()
	defer DefaultBugStore.mu.RUnlock()

	if status == "" || status == "ALL" {
		return append([]BugReport{}, DefaultBugStore.bugs...)
	}

	result := make([]BugReport, 0)
	for _, b := range DefaultBugStore.bugs {
		if b.Status == status {
			result = append(result, b)
		}
	}
	return result
}

func GetBugByID(id string) (BugReport, bool) {
	DefaultBugStore.mu.RLock()
	DefaultBugStore.Load()
	defer DefaultBugStore.mu.RUnlock()

	for _, b := range DefaultBugStore.bugs {
		if b.ID == id {
			return b, true
		}
	}
	return BugReport{}, false
}

func SaveOrUpdateBug(b BugReport) BugReport {
	DefaultBugStore.mu.Lock()
	DefaultBugStore.Load()
	defer DefaultBugStore.mu.Unlock()

	now := time.Now().UTC().Format(time.RFC3339)

	for i := range DefaultBugStore.bugs {
		if DefaultBugStore.bugs[i].Fingerprint == b.Fingerprint {
			DefaultBugStore.bugs[i].IncidentCount++
			DefaultBugStore.bugs[i].LastSeen = now
			DefaultBugStore.bugs[i].UpdatedAt = now
			DefaultBugStore.bugs[i].Severity = b.Severity
			DefaultBugStore.bugs[i].RootCause = b.RootCause
			DefaultBugStore.bugs[i].AffectedFiles = b.AffectedFiles
			DefaultBugStore.bugs[i].AffectedFunctions = b.AffectedFunctions
			DefaultBugStore.bugs[i].ProposedFix = b.ProposedFix
			DefaultBugStore.save()
			return DefaultBugStore.bugs[i]
		}
	}

	if b.ID == "" {
		buf := make([]byte, 4)
		_, _ = rand.Read(buf)
		b.ID = "BUG-" + hex.EncodeToString(buf)
	}

	b.IncidentCount = 1
	b.FirstSeen = now
	b.LastSeen = now
	b.CreatedAt = now
	b.UpdatedAt = now
	if b.Status == "" {
		b.Status = "WAITING_FOR_OWNER_APPROVAL"
	}

	DefaultBugStore.bugs = append([]BugReport{b}, DefaultBugStore.bugs...)
	DefaultBugStore.save()
	return b
}

func SetOwnerDecision(id string, approved bool, note string) (BugReport, bool) {
	DefaultBugStore.mu.Lock()
	DefaultBugStore.Load()
	defer DefaultBugStore.mu.Unlock()

	now := time.Now().UTC().Format(time.RFC3339)
	for i := range DefaultBugStore.bugs {
		if DefaultBugStore.bugs[i].ID == id {
			DefaultBugStore.bugs[i].OwnerDecision = &OwnerDecision{
				Approved:  approved,
				DecidedAt: now,
				Note:      note,
			}
			if approved {
				DefaultBugStore.bugs[i].Status = "APPROVED"
			} else {
				DefaultBugStore.bugs[i].Status = "REJECTED"
			}
			DefaultBugStore.bugs[i].UpdatedAt = now
			DefaultBugStore.save()
			return DefaultBugStore.bugs[i], true
		}
	}
	return BugReport{}, false
}

func UpdateBugWorkflowStatus(id string, status string, branch string, fixRes *FixResult) (BugReport, bool) {
	DefaultBugStore.mu.Lock()
	DefaultBugStore.Load()
	defer DefaultBugStore.mu.Unlock()

	now := time.Now().UTC().Format(time.RFC3339)
	for i := range DefaultBugStore.bugs {
		if DefaultBugStore.bugs[i].ID == id {
			DefaultBugStore.bugs[i].Status = status
			if branch != "" {
				DefaultBugStore.bugs[i].FixBranch = branch
			}
			if fixRes != nil {
				DefaultBugStore.bugs[i].FixResult = fixRes
			}
			DefaultBugStore.bugs[i].UpdatedAt = now
			DefaultBugStore.save()
			return DefaultBugStore.bugs[i], true
		}
	}
	return BugReport{}, false
}
