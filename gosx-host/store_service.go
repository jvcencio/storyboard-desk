package main

import "encoding/json"

// EditorService keeps the legacy HTTP bridge available for compatibility tests.
// The standalone editor uses ProjectStore directly and needs no Python listener.
type EditorService interface {
	State() (ServiceState, error)
	CreateProject(ServiceState, string, string, string) error
	InsertShot(ServiceState, string, string, string, int) error
	SaveShot(ServiceState, string, string, map[string]string) (ServiceProject, error)
}

func serviceProject(p ProjectDocument) ServiceProject {
	var shots []map[string]json.RawMessage
	_ = json.Unmarshal(p["shots"], &shots)
	return ServiceProject{ID: shotText(p, "id"), Title: shotText(p, "title"), Revision: documentRevision(p), Shots: shots}
}

func (s *ProjectStore) State() (ServiceState, error) {
	projects, err := s.List()
	state := ServiceState{App: "Storyboard Desk", Projects: []ServiceProject{}}
	if err != nil {
		return state, err
	}
	for _, p := range projects {
		state.Projects = append(state.Projects, serviceProject(p))
	}
	return state, nil
}

func (s *ProjectStore) CreateProject(_ ServiceState, title, brief, aspect string) error {
	_, err := s.Create(title, brief, aspect)
	return err
}

func (s *ProjectStore) InsertShot(_ ServiceState, id, anchor, position string, revision int) error {
	_, err := s.Insert(id, anchor, position, revision)
	return err
}

func (s *ProjectStore) SaveShot(state ServiceState, id, shotID string, fields map[string]string) (ServiceProject, error) {
	for _, p := range state.Projects {
		if p.ID != id {
			continue
		}
		patch := ProjectDocument{}
		for k, v := range fields {
			patch[k] = rawValue(v)
		}
		updated, err := s.UpdateShot(id, shotID, p.Revision, patch)
		if err != nil {
			return ServiceProject{}, err
		}
		return serviceProject(updated), nil
	}
	return ServiceProject{}, ErrNotFound
}
