package model

import "encoding/json"

// NormalizeValueDataForCanonical converts known structured payload structs to
// a JSON object before hashing. This makes an API-written CodeRef equivalent
// to a CLI-written JSON object and keeps the event hash stable when SQLite
// restores the structured Go type through Value.UnmarshalJSON.
func NormalizeValueDataForCanonical(value Value) Value {
	switch value.Data.(type) {
	case CodeRef, *CodeRef, GitRef, *GitRef, ArtifactDescriptor, *ArtifactDescriptor,
		MediaDescriptor, *MediaDescriptor, InlineSequence, *InlineSequence:
		encoded, err := json.Marshal(value.Data)
		if err != nil {
			return value
		}
		var normalized any
		if err := json.Unmarshal(encoded, &normalized); err == nil {
			value.Data = normalized
		}
	}
	return value
}
