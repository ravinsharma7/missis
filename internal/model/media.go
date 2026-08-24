package model

// MediaDescriptor is the structured payload used by the media rendering
// revision-2 contract. URI may point at a local path, remote URL, or content-addressed
// artifact. Consumers must decide whether and how to retrieve it.
//
// Kind is explicit so a renderer never has to infer executable behavior from
// a URL, MIME type, or arbitrary content.
type MediaDescriptor struct {
	Kind      ValueKind `json:"kind"`
	URI       string    `json:"uri"`
	MediaType string    `json:"media_type,omitempty"`
	Alt       string    `json:"alt,omitempty"`
	PosterURI string    `json:"poster_uri,omitempty"`
}
