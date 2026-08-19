package llm

import "testing"

func TestBuildChatRequestSessionID(t *testing.T) {
	tests := []struct {
		name    string
		session string
		want    *string
	}{
		{"empty session is omitted", "", nil},
		{"session is forwarded", "123456789", new("123456789")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := buildChatRequest(CompletionRequest{Model: "m", SessionID: tc.session}).SessionID
			switch {
			case tc.want == nil && got != nil:
				t.Errorf("SessionID = %q, want nil", *got)
			case tc.want != nil && got == nil:
				t.Errorf("SessionID = nil, want %q", *tc.want)
			case tc.want != nil && *got != *tc.want:
				t.Errorf("SessionID = %q, want %q", *got, *tc.want)
			}
		})
	}
}
