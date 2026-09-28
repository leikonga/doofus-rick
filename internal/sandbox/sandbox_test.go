package sandbox

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    []Tool
		wantErr string
	}{
		{
			name:  "comments and blanks skipped",
			input: "# header\n\n   \n  # indented comment\njq; jq; query JSON\n",
			want:  []Tool{{Package: "jq", Commands: []string{"jq"}, Purpose: "query JSON"}},
		},
		{
			name:  "empty commands",
			input: "font-noto; ; fonts\n",
			want:  []Tool{{Package: "font-noto", Purpose: "fonts"}},
		},
		{
			name:  "multiple commands and semicolon in purpose",
			input: "poppler-utils;pdftotext, pdftoppm ,;text; and images",
			want:  []Tool{{Package: "poppler-utils", Commands: []string{"pdftotext", "pdftoppm"}, Purpose: "text; and images"}},
		},
		{name: "missing fields", input: "jq; jq\n", wantErr: "line 1"},
		{name: "empty package", input: "# c\n ; jq; query\n", wantErr: "line 2"},
		{name: "package with space", input: "jq yq; jq; query\n", wantErr: "invalid package"},
		{name: "empty purpose", input: "jq; jq;  \n", wantErr: "no purpose"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(strings.NewReader(tt.input))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestEmbeddedManifestParses(t *testing.T) {
	tools, err := Manifest()
	if err != nil {
		t.Fatalf("Manifest: %v", err)
	}
	seen := map[string]bool{}
	for _, tool := range tools {
		if seen[tool.Package] {
			t.Errorf("duplicate package %q", tool.Package)
		}
		seen[tool.Package] = true
	}
	for _, pkg := range []string{"bash", "chromium", "imagemagick", "libcap"} {
		if !seen[pkg] {
			t.Errorf("manifest missing %q", pkg)
		}
	}
}

func TestAvailable(t *testing.T) {
	installed := map[string]bool{"jq": true, "psql": true}
	lookPath := func(cmd string) (string, error) {
		if installed[cmd] {
			return "/usr/bin/" + cmd, nil
		}
		return "", errors.New("not found")
	}
	tools := []Tool{
		{Package: "jq", Commands: []string{"jq"}, Purpose: "json"},
		{Package: "imagemagick", Commands: []string{"magick"}, Purpose: "images"},
		{Package: "postgresql-client", Commands: []string{"psql", "pg_dump"}, Purpose: "pg"},
		{Package: "font-noto", Purpose: "fonts"},
	}
	var got []string
	for _, tool := range availableWith(tools, lookPath) {
		got = append(got, tool.Package)
	}
	want := []string{"jq", "font-noto"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("available = %v, want %v", got, want)
	}
}
