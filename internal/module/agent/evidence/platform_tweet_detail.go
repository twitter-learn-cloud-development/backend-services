package evidence

const PlatformTweetDetailSchema = "platform.tweet_detail.v1"

// PlatformTweetDetailResult is the authoritative projection returned by the
// first-party tweet detail tool. Runtime verification must use this payload,
// never the model-facing text rendering.
type PlatformTweetDetailResult struct {
	Schema string                        `json:"schema"`
	Items  []PlatformTweetSearchEvidence `json:"items"`
}
