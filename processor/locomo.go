// SPDX-License-Identifier: MIT

package processor

import (
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	jsoniter "github.com/json-iterator/go"
)

const (
	locomoAssumptions = "Each unique commit reachable from HEAD, including merge commits, is priced as one benchmark task. Commit size is not weighted."
	locomoDisclaimer  = "Benchmark transfer is uncertain; this excludes product discovery and requirements recovery."
)

var Locomo = false
var LocomoAgent = ""
var LocomoCostPerTask = 0.0
var LocomoModel = ""
var LocomoOverhead = 5.0

type locomoProfile struct {
	Source           string
	Version          string
	Agent            string
	Model            string
	CostPerTaskCents int64
	URL              string
}

func artificialAnalysisLocomoProfile() locomoProfile {
	return locomoProfile{
		Source:           "Artificial Analysis Coding Agent Index",
		Version:          "v1.3",
		Agent:            "Codex",
		Model:            "GPT-5.6 Luna (xhigh)",
		CostPerTaskCents: 121,
		URL:              "https://artificialanalysis.ai/agents/coding-agents",
	}
}

func resolveLocomoProfile(agent, model string, costPerTaskUSD float64) (locomoProfile, error) {
	profile := artificialAnalysisLocomoProfile()
	agent, model = strings.TrimSpace(agent), strings.TrimSpace(model)
	if agent == "" && model == "" && costPerTaskUSD == 0 {
		return profile, nil
	}
	if agent == "" || model == "" || costPerTaskUSD == 0 {
		return locomoProfile{}, errors.New("--locomo-agent, --locomo-model, and --locomo-cost-per-task must be set together")
	}
	cents := costPerTaskUSD * 100
	roundedCents := math.Round(cents)
	if costPerTaskUSD <= 0 || math.IsNaN(cents) || math.IsInf(cents, 0) ||
		math.Abs(cents-roundedCents) > 1e-9 || roundedCents >= float64(math.MaxInt64) {
		return locomoProfile{}, errors.New("--locomo-cost-per-task must be a positive USD amount with at most two decimal places")
	}
	if agent != "" {
		profile.Agent = agent
	}
	if model != "" {
		profile.Model = model
	}
	profile.CostPerTaskCents = int64(roundedCents)
	return profile, nil
}

type locomoEstimate struct {
	Commits            int
	APICostCents       int64
	OverheadMultiplier float64
	EstimatedCostUSD   float64
	Profile            locomoProfile
}

func estimateLocomo(repoPath string, overhead float64, profile locomoProfile) (locomoEstimate, error) {
	if err := validateLocomoOverhead(overhead); err != nil {
		return locomoEstimate{}, err
	}
	if _, err := os.Stat(repoPath); err != nil {
		return locomoEstimate{}, fmt.Errorf("read repository path: %w", err)
	}

	EnableGc()
	repo, err := git.PlainOpenWithOptions(repoPath, &git.PlainOpenOptions{
		DetectDotGit:          true,
		EnableDotGitCommonDir: true,
	})
	if err != nil {
		return locomoEstimate{}, fmt.Errorf("open git repository: %w", err)
	}
	head, err := repo.Head()
	if err != nil {
		if errors.Is(err, plumbing.ErrReferenceNotFound) {
			return priceLocomo(0, overhead, profile)
		}
		return locomoEstimate{}, fmt.Errorf("read HEAD: %w", err)
	}
	commits, err := countReachableCommits(repo, head.Hash())
	if err != nil {
		return locomoEstimate{}, fmt.Errorf("count commits: %w", err)
	}
	return priceLocomo(commits, overhead, profile)
}

func countReachableCommits(repo *git.Repository, head plumbing.Hash) (int, error) {
	shallow, err := repo.Storer.Shallow()
	if err != nil {
		return 0, err
	}
	boundaries := make(map[plumbing.Hash]struct{}, len(shallow))
	for _, hash := range shallow {
		boundaries[hash] = struct{}{}
	}

	seen := make(map[plumbing.Hash]struct{})
	pending := []plumbing.Hash{head}
	for len(pending) > 0 {
		hash := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if _, ok := seen[hash]; ok {
			continue
		}
		seen[hash] = struct{}{}
		if _, stop := boundaries[hash]; stop {
			continue
		}
		commit, err := repo.CommitObject(hash)
		if err != nil {
			return 0, err
		}
		pending = append(pending, commit.ParentHashes...)
	}
	return len(seen), nil
}

func priceLocomo(commits int, overhead float64, profile locomoProfile) (locomoEstimate, error) {
	if err := validateLocomoOverhead(overhead); err != nil {
		return locomoEstimate{}, err
	}
	if profile.CostPerTaskCents < 0 || commits < 0 ||
		(profile.CostPerTaskCents > 0 && int64(commits) > math.MaxInt64/profile.CostPerTaskCents) {
		return locomoEstimate{}, errors.New("LOCOMO estimate is too large to represent")
	}
	apiCents := int64(commits) * profile.CostPerTaskCents
	estimateUSD := float64(apiCents) / 100 * overhead
	if err := validateLocomoMoney(estimateUSD); err != nil {
		return locomoEstimate{}, err
	}
	return locomoEstimate{
		Commits:            commits,
		APICostCents:       apiCents,
		OverheadMultiplier: overhead,
		EstimatedCostUSD:   estimateUSD,
		Profile:            profile,
	}, nil
}

func validateLocomoOverhead(overhead float64) error {
	if overhead < 1 || math.IsNaN(overhead) || math.IsInf(overhead, 0) {
		return errors.New("--locomo-overhead must be a finite number >= 1")
	}
	return nil
}

func validateLocomoMoney(value float64) error {
	cents := value * 100
	if math.IsNaN(cents) || math.IsInf(cents, 0) || math.Round(cents) >= float64(math.MaxInt64) {
		return errors.New("LOCOMO estimate is too large to represent")
	}
	return nil
}

func runLocomoReport(repoPath string) error {
	profile, err := resolveLocomoProfile(LocomoAgent, LocomoModel, LocomoCostPerTask)
	if err != nil {
		return err
	}
	estimate, err := estimateLocomo(repoPath, LocomoOverhead, profile)
	if err != nil {
		return err
	}
	out, err := renderLocomo(estimate)
	if err != nil {
		return err
	}
	if FileOutput == "" {
		fmt.Print(out)
		return nil
	}
	if err := os.WriteFile(FileOutput, []byte(out), 0644); err != nil {
		return err
	}
	fmt.Println("results written to " + FileOutput)
	return nil
}

func renderLocomo(e locomoEstimate) (string, error) {
	switch strings.ToLower(Format) {
	case "", "tabular", "wide":
		return renderLocomoTabular(e), nil
	case "json":
		return renderLocomoJSON(e)
	default:
		return "", fmt.Errorf("unsupported --format %q for --locomo (supported: tabular, json)", Format)
	}
}

func formatLocomoCents(cents int64) string {
	return commaFmt(cents/100) + fmt.Sprintf(".%02d", cents%100)
}

func formatLocomoMoney(value float64) string {
	return formatLocomoCents(int64(math.Round(value * 100)))
}

func formatLocomoMultiplier(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64) + "x"
}

func renderLocomoTabular(e locomoEstimate) string {
	var b strings.Builder
	fmt.Fprintln(&b, "LOCOMO benchmark estimate (Git history)")
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "Commits                          %s\n", commaFmt(int64(e.Commits)))
	fmt.Fprintf(&b, "Benchmark cost per task          $%s\n", formatLocomoCents(e.Profile.CostPerTaskCents))
	fmt.Fprintf(&b, "Benchmark API subtotal           $%s\n", formatLocomoCents(e.APICostCents))
	fmt.Fprintf(&b, "Project overhead                 %s\n", formatLocomoMultiplier(e.OverheadMultiplier))
	fmt.Fprintf(&b, "Estimated LOCOMO cost            $%s\n", formatLocomoMoney(e.EstimatedCostUSD))
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "Benchmark: %s %s · %s + %s\n", e.Profile.Source, e.Profile.Version, e.Profile.Agent, e.Profile.Model)
	fmt.Fprintf(&b, "Source: %s\n", e.Profile.URL)
	fmt.Fprintf(&b, "Assumptions: %s\n", locomoAssumptions)
	fmt.Fprintf(&b, "Caution: %s\n", locomoDisclaimer)
	return b.String()
}

type locomoJSONBenchmark struct {
	Source         string  `json:"source"`
	Version        string  `json:"version"`
	Agent          string  `json:"agent"`
	Model          string  `json:"model"`
	CostPerTaskUSD float64 `json:"costPerTaskUSD"`
	URL            string  `json:"url"`
}

type locomoJSONDoc struct {
	Report                    string              `json:"report"`
	Commits                   int                 `json:"commits"`
	Benchmark                 locomoJSONBenchmark `json:"benchmark"`
	APICostUSD                float64             `json:"apiCostUSD"`
	ProjectOverheadMultiplier float64             `json:"projectOverheadMultiplier"`
	EstimatedCostUSD          float64             `json:"estimatedCostUSD"`
	Assumptions               string              `json:"assumptions"`
	Disclaimer                string              `json:"disclaimer"`
}

func locomoJSON(e locomoEstimate) locomoJSONDoc {
	return locomoJSONDoc{
		Report:  "locomo",
		Commits: e.Commits,
		Benchmark: locomoJSONBenchmark{
			Source: e.Profile.Source, Version: e.Profile.Version,
			Agent: e.Profile.Agent, Model: e.Profile.Model,
			CostPerTaskUSD: float64(e.Profile.CostPerTaskCents) / 100, URL: e.Profile.URL,
		},
		APICostUSD:                float64(e.APICostCents) / 100,
		ProjectOverheadMultiplier: e.OverheadMultiplier,
		EstimatedCostUSD:          math.Round(e.EstimatedCostUSD*100) / 100,
		Assumptions:               locomoAssumptions,
		Disclaimer:                locomoDisclaimer,
	}
}

func renderLocomoJSON(e locomoEstimate) (string, error) {
	buf, err := jsoniter.ConfigCompatibleWithStandardLibrary.MarshalIndent(locomoJSON(e), "", "  ")
	if err != nil {
		return "", err
	}
	return string(buf) + "\n", nil
}
