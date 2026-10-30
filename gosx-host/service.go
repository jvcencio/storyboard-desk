package main

import (
    "bytes"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "net/url"
    "strings"
    "time"
)

// ServiceClient is the compatibility boundary for the existing local Python service.
// It deliberately does not own project data or relax the service's revision checks.
type ServiceClient struct {
    base string
    client *http.Client
}

type ServiceState struct {
    App string `json:"app"`
    Token string `json:"token"`
    Projects []ServiceProject `json:"projects"`
}

type ServiceProject struct {
    ID string `json:"id"`
    Title string `json:"title"`
    Revision int `json:"revision"`
    Shots []map[string]json.RawMessage `json:"shots"`
}

func NewServiceClient(raw string) (*ServiceClient, error) {
    base, err := validateURL(raw)
    if err != nil { return nil, err }
    u, _ := url.Parse(base)
    u.RawQuery = ""
    return &ServiceClient{base: strings.TrimSuffix(u.String(), "/"), client: &http.Client{
        Timeout: 10 * time.Second,
        Transport: &http.Transport{Proxy: nil},
        CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
    }}, nil
}

func (c *ServiceClient) request(method, path, token string, body any, result any) error {
    var data io.Reader
    if body != nil {
        raw, err := json.Marshal(body)
        if err != nil { return err }
        data = bytes.NewReader(raw)
    }
    req, err := http.NewRequest(method, c.base+path, data)
    if err != nil { return err }
    if body != nil { req.Header.Set("Content-Type", "application/json"); req.Header.Set("X-Storyboard-Token", token) }
    resp, err := c.client.Do(req)
    if err != nil { return err }
    defer resp.Body.Close()
    if resp.StatusCode != http.StatusOK {
        var detail struct { Error string `json:"error"` }
        _ = json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&detail)
        if detail.Error == "" { detail.Error = http.StatusText(resp.StatusCode) }
        return fmt.Errorf("Storyboard Desk: %s (HTTP %d)", detail.Error, resp.StatusCode)
    }
    return json.NewDecoder(io.LimitReader(resp.Body, 64<<20)).Decode(result)
}

func (c *ServiceClient) State() (ServiceState, error) {
    var state ServiceState
    err := c.request(http.MethodGet, "/api/state", "", nil, &state)
    if err == nil && (state.App != "Storyboard Desk" || state.Token == "") { err = fmt.Errorf("the local listener is not Storyboard Desk") }
    return state, err
}

// SaveShot copies and patches the existing shot rather than reconstructing it,
// preserving fields the new UI does not yet understand.
func (c *ServiceClient) SaveShot(state ServiceState, projectID, shotID string, fields map[string]string) (ServiceProject, error) {
    var updated ServiceProject
    for _, project := range state.Projects {
        if project.ID != projectID { continue }
        shots := make([]map[string]json.RawMessage, len(project.Shots))
        found := false
        for i, shot := range project.Shots {
            shots[i] = make(map[string]json.RawMessage, len(shot))
            for k, v := range shot { shots[i][k] = v }
            var id string
            _ = json.Unmarshal(shot["id"], &id)
            if id != shotID { continue }
            found = true
            for k, v := range fields {
                switch k { case "title", "purpose", "composition", "action", "camera": default: return updated, fmt.Errorf("unsupported shot field %q", k) }
                shots[i][k], _ = json.Marshal(v)
            }
        }
        if !found { return updated, fmt.Errorf("shot not found") }
        var response struct { Project ServiceProject `json:"project"` }
        err := c.request(http.MethodPost, "/api/save", state.Token, map[string]any{"id": project.ID, "revision": project.Revision, "shots": shots}, &response)
        return response.Project, err
    }
    return updated, fmt.Errorf("project not found")
}
