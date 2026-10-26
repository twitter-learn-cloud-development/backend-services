package service

import (
	"errors"
	"testing"
)

func TestValidateMediaURLs(t *testing.T) {
	service := &TweetService{}
	cases := []struct {
		name string
		url  string
		want error
	}{
		{name: "uploaded same-origin media", url: "/media/twitter-media/20260927/550e8400-e29b-41d4-a716-446655440000.png"},
		{name: "existing absolute media", url: "https://cdn.example.com/image.png"},
		{name: "empty", url: ""},
		{name: "protocol relative host", url: "//example.com/media/twitter-media/image.png", want: ErrInvalidMediaURL},
		{name: "wrong bucket", url: "/media/other-bucket/image.png", want: ErrInvalidMediaURL},
		{name: "missing object", url: "/media/twitter-media/", want: ErrInvalidMediaURL},
		{name: "traversal", url: "/media/twitter-media/../private/image.png", want: ErrInvalidMediaURL},
		{name: "encoded traversal", url: "/media/twitter-media/%2e%2e/private/image.png", want: ErrInvalidMediaURL},
		{name: "query", url: "/media/twitter-media/image.png?download=true", want: ErrInvalidMediaURL},
		{name: "fragment", url: "/media/twitter-media/image.png#fragment", want: ErrInvalidMediaURL},
		{name: "backslash", url: `/media/twitter-media/..\private.png`, want: ErrInvalidMediaURL},
		{name: "unsafe scheme", url: "javascript:alert(1)", want: ErrInvalidMediaURL},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := service.validateMediaURLs([]string{tc.url})
			if !errors.Is(err, tc.want) {
				t.Fatalf("validateMediaURLs(%q) = %v, want %v", tc.url, err, tc.want)
			}
		})
	}
}
