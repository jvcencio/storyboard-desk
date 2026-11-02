package main

import "encoding/json"

// ServiceState is the compact, read-only view used by the GoSX board preview.
type ServiceState struct {
	App      string           `json:"app"`
	Token    string           `json:"token,omitempty"`
	Projects []ServiceProject `json:"projects"`
}

type ServiceProject struct {
	ID       string                       `json:"id"`
	Title    string                       `json:"title"`
	Revision int                          `json:"revision"`
	Shots    []map[string]json.RawMessage `json:"shots"`
}

func shotText(shot map[string]json.RawMessage, name string) string {
	var s string
	_ = json.Unmarshal(shot[name], &s)
	return s
}

func (s *ProjectStore) State() (ServiceState, error) {
	projects, err := s.List()
	state := ServiceState{App: "Storyboard Desk", Projects: []ServiceProject{}}
	if err != nil {
		return state, err
	}
	for _, p := range projects {
		var shots []map[string]json.RawMessage
		_ = json.Unmarshal(p["shots"], &shots)
		state.Projects = append(state.Projects, ServiceProject{ID: shotText(p, "id"), Title: shotText(p, "title"), Revision: documentRevision(p), Shots: shots})
	}
	return state, nil
}
