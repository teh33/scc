// SPDX-License-Identifier: MIT

package processor

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

func TestResolveLocomoProfile(t *testing.T) {
	profile, err := resolveLocomoProfile("", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if profile.Agent != "Codex" || profile.Model != "GPT-5.6 Luna (xhigh)" || profile.CostPerTaskCents != 121 {
		t.Fatalf("default profile = %#v", profile)
	}

	profile, err = resolveLocomoProfile("Claude Code", "Sonnet 4.6", 0.42)
	if err != nil {
		t.Fatal(err)
	}
	if profile.Agent != "Claude Code" || profile.Model != "Sonnet 4.6" || profile.CostPerTaskCents != 42 {
		t.Fatalf("custom profile = %#v", profile)
	}
}

func TestResolveLocomoProfileRejectsPartialOrInvalidOverrides(t *testing.T) {
	for _, tc := range []struct {
		name, agent, model string
		cost               float64
	}{
		{name: "cost only", cost: 1},
		{name: "agent only", agent: "Codex"},
		{name: "missing model", agent: "Codex", cost: 1},
		{name: "missing agent", model: "GPT", cost: 1},
		{name: "missing cost", agent: "Codex", model: "GPT"},
		{name: "negative cost", agent: "Codex", model: "GPT", cost: -1},
		{name: "fractional cent", agent: "Codex", model: "GPT", cost: 1.001},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := resolveLocomoProfile(tc.agent, tc.model, tc.cost); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestPriceLocomoUsesExactCommitCents(t *testing.T) {
	estimate, err := priceLocomo(1603, 5, artificialAnalysisLocomoProfile())
	if err != nil {
		t.Fatal(err)
	}
	if estimate.Commits != 1603 || estimate.APICostCents != 193963 {
		t.Fatalf("estimate = %#v", estimate)
	}
	if math.Abs(estimate.EstimatedCostUSD-9698.15) > 1e-9 {
		t.Fatalf("LOCOMO estimate = %v, want 9698.15", estimate.EstimatedCostUSD)
	}
}

func TestEstimateLocomoCountsLinearHistory(t *testing.T) {
	dir := makeFixtureRepo(t, []map[string]string{
		{"a.go": "package a\n"},
		{"a.go": "package a\nfunc A() {}\n"},
		{"a.go": "package a\nfunc A() {}\nfunc B() {}\n"},
	})
	estimate, err := estimateLocomo(dir, 5, artificialAnalysisLocomoProfile())
	if err != nil {
		t.Fatal(err)
	}
	if estimate.Commits != 3 {
		t.Fatalf("commits = %d, want 3", estimate.Commits)
	}
}

func TestEstimateLocomoCountsMergeDAGOnce(t *testing.T) {
	dir := makeFixtureRepo(t, []map[string]string{{"a.go": "package a\n"}})
	repo, err := git.PlainOpen(dir)
	if err != nil {
		t.Fatal(err)
	}
	base, err := repo.Head()
	if err != nil {
		t.Fatal(err)
	}
	tree, err := repo.CommitObject(base.Hash())
	if err != nil {
		t.Fatal(err)
	}
	when := time.Date(2025, 1, 2, 12, 0, 0, 0, time.UTC)
	left := commitWithParents(t, repo, tree.TreeHash, "left", when, base.Hash())
	right := commitWithParents(t, repo, tree.TreeHash, "right", when.Add(time.Hour), base.Hash())
	merge := commitWithParents(t, repo, tree.TreeHash, "merge", when.Add(2*time.Hour), left, right)

	got, err := countReachableCommits(repo, merge)
	if err != nil {
		t.Fatal(err)
	}
	if got != 4 {
		t.Fatalf("commits = %d, want 4", got)
	}
}

func commitWithParents(t *testing.T, repo *git.Repository, tree plumbing.Hash, message string, when time.Time, parents ...plumbing.Hash) plumbing.Hash {
	t.Helper()
	commit := &object.Commit{
		Author:       object.Signature{Name: "Test", Email: "test@example.com", When: when},
		Committer:    object.Signature{Name: "Test", Email: "test@example.com", When: when},
		Message:      message,
		TreeHash:     tree,
		ParentHashes: parents,
	}
	obj := repo.Storer.NewEncodedObject()
	if err := commit.Encode(obj); err != nil {
		t.Fatal(err)
	}
	hash, err := repo.Storer.SetEncodedObject(obj)
	if err != nil {
		t.Fatal(err)
	}
	return hash
}

func TestEstimateLocomoHandlesEmptyRepository(t *testing.T) {
	dir := t.TempDir()
	if _, err := git.PlainInit(dir, false); err != nil {
		t.Fatal(err)
	}
	estimate, err := estimateLocomo(dir, 5, artificialAnalysisLocomoProfile())
	if err != nil {
		t.Fatal(err)
	}
	if estimate.Commits != 0 || estimate.APICostCents != 0 || estimate.EstimatedCostUSD != 0 {
		t.Fatalf("estimate = %#v", estimate)
	}
}

func TestEstimateLocomoStopsAtShallowBoundary(t *testing.T) {
	dir := makeFixtureRepo(t, []map[string]string{
		{"a.go": "package a\n"},
		{"a.go": "package a\nfunc A() {}\n"},
		{"a.go": "package a\nfunc A() {}\nfunc B() {}\n"},
	})
	repo, err := git.PlainOpen(dir)
	if err != nil {
		t.Fatal(err)
	}
	head, err := repo.Head()
	if err != nil {
		t.Fatal(err)
	}
	commit, err := repo.CommitObject(head.Hash())
	if err != nil {
		t.Fatal(err)
	}
	boundary := commit.ParentHashes[0]
	if err := repo.Storer.SetShallow([]plumbing.Hash{boundary}); err != nil {
		t.Fatal(err)
	}
	got, err := countReachableCommits(repo, head.Hash())
	if err != nil {
		t.Fatal(err)
	}
	if got != 2 {
		t.Fatalf("commits = %d, want 2", got)
	}
}

func TestEstimateLocomoSupportsLinkedWorktrees(t *testing.T) {
	repoDir := makeFixtureRepo(t, []map[string]string{
		{"a.go": "package a\n"},
		{"a.go": "package a\nfunc A() {}\n"},
	})
	linked := t.TempDir()
	admin := filepath.Join(repoDir, ".git", "worktrees", "linked")
	if err := os.MkdirAll(admin, 0o755); err != nil {
		t.Fatal(err)
	}
	head, err := os.ReadFile(filepath.Join(repoDir, ".git", "HEAD"))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		filepath.Join(linked, ".git"):     "gitdir: " + admin + "\n",
		filepath.Join(admin, "commondir"): "../..\n",
		filepath.Join(admin, "gitdir"):    filepath.Join(linked, ".git") + "\n",
		filepath.Join(admin, "HEAD"):      string(head),
	}
	for name, content := range files {
		if err := os.WriteFile(name, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	estimate, err := estimateLocomo(linked, 5, artificialAnalysisLocomoProfile())
	if err != nil {
		t.Fatal(err)
	}
	if estimate.Commits != 2 {
		t.Fatalf("worktree commits = %d, want 2", estimate.Commits)
	}
}

func TestEstimateLocomoFindsRepositoryFromNestedPath(t *testing.T) {
	dir := makeFixtureRepo(t, []map[string]string{{"nested/a.go": "package a\n"}})
	estimate, err := estimateLocomo(filepath.Join(dir, "nested"), 5, artificialAnalysisLocomoProfile())
	if err != nil {
		t.Fatal(err)
	}
	if estimate.Commits != 1 {
		t.Fatalf("commits = %d, want 1", estimate.Commits)
	}
}

func TestRenderLocomoTabular(t *testing.T) {
	estimate, err := priceLocomo(1603, 1.234, artificialAnalysisLocomoProfile())
	if err != nil {
		t.Fatal(err)
	}
	out := renderLocomoTabular(estimate)
	for _, want := range []string{
		"LOCOMO benchmark estimate (Git history)",
		"Commits                          1,603",
		"Benchmark cost per task          $1.21",
		"Benchmark API subtotal           $1,939.63",
		"Project overhead                 1.234x",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestRenderLocomoJSONContract(t *testing.T) {
	estimate, err := priceLocomo(10, 5, artificialAnalysisLocomoProfile())
	if err != nil {
		t.Fatal(err)
	}
	out, err := renderLocomoJSON(estimate)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatal(err)
	}
	if doc["report"] != "locomo" || doc["commits"] != float64(10) || doc["apiCostUSD"] != 12.1 || doc["estimatedCostUSD"] != 60.5 {
		t.Fatalf("document = %#v", doc)
	}
	benchmark := doc["benchmark"].(map[string]any)
	if benchmark["costPerTaskUSD"] != 1.21 {
		t.Fatalf("benchmark = %#v", benchmark)
	}
	if _, ok := benchmark["codingAgentIndex"]; ok {
		t.Error("unused codingAgentIndex should be absent")
	}
}

func TestPriceLocomoRejectsInvalidInputs(t *testing.T) {
	for _, tc := range []struct {
		name     string
		commits  int
		overhead float64
	}{
		{name: "negative commits", commits: -1, overhead: 5},
		{name: "small overhead", commits: 1, overhead: 0.5},
		{name: "NaN overhead", commits: 1, overhead: math.NaN()},
		{name: "overflow", commits: 2, overhead: 1e308},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := priceLocomo(tc.commits, tc.overhead, artificialAnalysisLocomoProfile()); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestRenderLocomoRejectsCSV(t *testing.T) {
	saved := Format
	Format = "csv"
	t.Cleanup(func() { Format = saved })
	if _, err := renderLocomo(locomoEstimate{}); err == nil || !strings.Contains(err.Error(), "supported: tabular, json") {
		t.Fatalf("unexpected error: %v", err)
	}
}
