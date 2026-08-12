package service

import (
	"errors"
	"testing"
)

func TestPlatformTweetOrdinal(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		input string
		want  int
	}{
		{input: "第二条详细内容", want: 2},
		{input: "show me the third result", want: 3},
		{input: "expand the 4th item", want: 4},
	} {
		got, ok := platformTweetOrdinal(test.input)
		if !ok || got != test.want {
			t.Fatalf("platformTweetOrdinal(%q) = %d, %v, want %d, true", test.input, got, ok, test.want)
		}
	}
}

func TestParsePlatformTweetReferenceRejectsLegacyAndInvalidPaths(t *testing.T) {
	t.Parallel()
	if _, ok := parsePlatformTweetReference("/tweet/42"); ok {
		t.Fatal("legacy display URL was accepted as trusted runtime reference")
	}
	if _, ok := parsePlatformTweetReference("/tweets/not-a-number"); ok {
		t.Fatal("invalid tweet reference was accepted")
	}
	reference, ok := parsePlatformTweetReference("/tweets/9007199254740993")
	if !ok || reference.TweetID != "9007199254740993" {
		t.Fatalf("trusted reference = %+v, %v", reference, ok)
	}
}

func TestPlatformTweetReferenceErrorsRemainDistinct(t *testing.T) {
	t.Parallel()
	if errors.Is(ErrPlatformTweetReferenceAmbiguous, ErrPlatformTweetReferenceUntrusted) {
		t.Fatal("ambiguous and untrusted reference errors must remain distinct")
	}
}
