package agent

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/disgoorg/disgo/discord"
	"github.com/leikonga/doofus-rick/internal/llm"
)

const (
	maxImages          = 5
	maxDocAttachments  = 4
	maxTextAttachBytes = 100 * 1024
)

var textAttachmentExts = map[string]bool{
	".txt": true, ".md": true, ".markdown": true, ".json": true,
	".yaml": true, ".yml": true, ".csv": true, ".log": true, ".toml": true,
	".ini": true, ".xml": true, ".html": true, ".css": true, ".sh": true,
	".go": true, ".py": true, ".js": true, ".ts": true, ".jsx": true, ".tsx": true,
	".java": true, ".c": true, ".h": true, ".cpp": true, ".rs": true, ".rb": true,
	".sql": true, ".diff": true, ".patch": true,
}

func isImageAttachment(att discord.Attachment) bool {
	return att.ContentType != nil && strings.HasPrefix(*att.ContentType, "image/")
}

func isPDFAttachment(att discord.Attachment) bool {
	return att.ContentType != nil && *att.ContentType == "application/pdf"
}

func isTextAttachment(att discord.Attachment) bool {
	if att.ContentType != nil {
		ct := strings.ToLower(*att.ContentType)
		if strings.HasPrefix(ct, "text/") || ct == "application/json" || ct == "application/xml" || ct == "application/x-yaml" {
			return true
		}
	}
	return textAttachmentExts[strings.ToLower(filepath.Ext(att.Filename))]
}

// unsupportedLabel is the inline placeholder shown to the model for an
// attachment that was not turned into an image or file part.
func unsupportedLabel(att discord.Attachment) string {
	return "(sent: " + att.Filename + ")"
}

func fetchAttachmentText(ctx context.Context, url string, maxBytes int64) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status %d fetching attachment", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// attachmentResult is the outcome of sorting a message's attachments into
// what the model can directly consume versus what only gets a text placeholder.
type attachmentResult struct {
	imageURLs   []string
	fileParts   []llm.ContentPart
	unsupported []string
}

// classifyAttachments sorts attachments into image URLs, file parts, and
// unsupported placeholders, capping each category per message.
func classifyAttachments(ctx context.Context, atts []discord.Attachment) attachmentResult {
	var res attachmentResult
	for _, att := range atts {
		switch {
		case isImageAttachment(att):
			if len(res.imageURLs) < maxImages {
				res.imageURLs = append(res.imageURLs, att.URL)
				continue
			}
		case isPDFAttachment(att):
			if len(res.fileParts) < maxDocAttachments {
				res.fileParts = append(res.fileParts, llm.FileContentPart(att.Filename, att.URL))
				continue
			}
		case isTextAttachment(att) && att.Size <= maxTextAttachBytes:
			if len(res.fileParts) < maxDocAttachments {
				text, err := fetchAttachmentText(ctx, att.URL, maxTextAttachBytes)
				if err != nil {
					slog.Warn("failed to fetch text attachment", "error", err, "filename", att.Filename)
					break
				}
				dataURL := "data:text/plain;base64," + base64.StdEncoding.EncodeToString([]byte(text))
				res.fileParts = append(res.fileParts, llm.FileContentPart(att.Filename, dataURL))
				continue
			}
		}
		res.unsupported = append(res.unsupported, unsupportedLabel(att))
	}
	return res
}
