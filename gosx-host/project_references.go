package main

import "fmt"

func (s *ProjectStore) ReferenceNote(id string, rev, index int, operation, title, link, notes string) (ProjectDocument, error) {
	return s.mutate(id, rev, func(p ProjectDocument) error {
		refs := list(p, "references")
		switch operation {
		case "add":
			refs = append(refs, ProjectDocument{"title": rawValue(title), "url": rawValue(link), "notes": rawValue(notes)})
		case "update", "remove":
			if index < 0 || index >= len(refs) {
				return fmt.Errorf("reference list changed; choose the note again")
			}
			if operation == "remove" {
				refs = append(refs[:index], refs[index+1:]...)
			} else {
				refs[index]["title"], refs[index]["url"], refs[index]["notes"] = rawValue(title), rawValue(link), rawValue(notes)
			}
		default:
			return fmt.Errorf("unknown reference operation")
		}
		if err := validateReferences(rawValue(refs)); err != nil {
			return err
		}
		p["references"] = rawValue(refs)
		return nil
	})
}
