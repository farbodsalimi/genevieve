package main

import (
	"encoding/json"
	"slices"

	"github.com/farbodsalimi/genevieve/pkg/graph"
	"github.com/farbodsalimi/genevieve/pkg/llm"
)

const maxRevisions = 2

type SpecialistReport struct {
	Specialist string
	Report     string
}

type NodeFailure struct {
	Node  string
	Error string
}

type ReportObservation struct {
	Title       string   `json:"title"`
	Summary     string   `json:"summary"`
	Findings    []string `json:"findings"`
	Risks       []string `json:"risks"`
	Limitations []string `json:"limitations"`
}

type State struct {
	Query      string
	Accepted   bool
	IntakeNote string
	Reports    []SpecialistReport
	Failures   []NodeFailure
	Draft      ReportObservation
	DraftJSON  string
	Review     string
	Revisions  int
	Usage      llm.Usage
	Streamed   string
}

// Clone gives parallel nodes independent backing arrays.
func (s State) Clone() State {
	s.Reports = slices.Clone(s.Reports)
	s.Failures = slices.Clone(s.Failures)
	s.Draft.Findings = slices.Clone(s.Draft.Findings)
	s.Draft.Risks = slices.Clone(s.Draft.Risks)
	s.Draft.Limitations = slices.Clone(s.Draft.Limitations)
	return s
}

type Update struct {
	Accepted   *bool
	IntakeNote string
	Report     *SpecialistReport
	Failure    *NodeFailure
	Draft      *ReportObservation
	DraftJSON  string
	Review     string
	Revised    bool
	Usage      llm.Usage
	Streamed   string
}

func reducer() graph.Reducer[State, Update] {
	return graph.ReducerFunc[State, Update](func(s State, u Update) (State, error) {
		next := s.Clone()
		if u.Accepted != nil {
			next.Accepted = *u.Accepted
		}
		if u.IntakeNote != "" {
			next.IntakeNote = u.IntakeNote
		}
		if u.Report != nil {
			next.Reports = append(next.Reports, *u.Report)
		}
		if u.Failure != nil {
			next.Failures = append(next.Failures, *u.Failure)
		}
		if u.Draft != nil {
			next.Draft = *u.Draft
			next.DraftJSON = u.DraftJSON
		}
		if u.Review != "" {
			next.Review = u.Review
		}
		if u.Revised {
			next.Revisions++
		}
		next.Usage = next.Usage.Add(u.Usage)
		next.Streamed += u.Streamed
		return next, nil
	})
}

func decodeObservation(raw json.RawMessage) (ReportObservation, error) {
	var report ReportObservation
	err := json.Unmarshal(raw, &report)
	return report, err
}
