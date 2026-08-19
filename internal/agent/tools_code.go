package agent

import (
	"context"
	"fmt"

	"github.com/leikonga/doofus-rick/internal/llm"
)

var errRepoNotCloned = fmt.Errorf("repo not found, clone it into RICK_REPO_DIR via sys_shell first")

var errRepoBusy = fmt.Errorf("another edit is in progress on the repo, retry shortly")

type codeReadIn struct {
	Path   string `json:"path" jsonschema:"required,description=Path to the file, relative to the repo root."`
	Offset int    `json:"offset" jsonschema:"description=0-based line number to start from. Omit to start at the beginning."`
	Limit  int    `json:"limit" jsonschema:"description=Maximum number of lines to return. Omit for no limit."`
}

func (a *Agent) codeReadTool() llm.Tool {
	return llm.NewTool("code_read", "Read a file from Rick's own source checkout, cat -n style with line numbers.",
		func(_ context.Context, in codeReadIn) (llm.Result, error) {
			if a.codeedit == nil {
				return llm.Result{}, errRepoNotCloned
			}
			if !a.repoMu.TryRLock() {
				return llm.Result{}, errRepoBusy
			}
			defer a.repoMu.RUnlock()

			content, err := a.codeedit.Read(in.Path, in.Offset, in.Limit)
			if err != nil {
				return llm.Result{}, err
			}
			return llm.Result{Content: content}, nil
		})
}

type codeEditIn struct {
	Command    string `json:"command" jsonschema:"required,enum=write,enum=str_replace,enum=insert,description=Which edit operation to perform."`
	Path       string `json:"path" jsonschema:"required,description=Path to the file, relative to the repo root."`
	FileText   string `json:"file_text" jsonschema:"description=Full file content. Required for command=write."`
	OldStr     string `json:"old_str" jsonschema:"description=Exact text to replace. Required for command=str_replace."`
	NewStr     string `json:"new_str" jsonschema:"description=Replacement text for command=str_replace, or the line to insert for command=insert."`
	InsertLine int    `json:"insert_line" jsonschema:"description=Line number after which to insert, 0 for the beginning of the file. Required for command=insert."`
}

func (a *Agent) codeEditTool() llm.Tool {
	return llm.NewTool("code_edit", "Edit a file in Rick's own source checkout. command=write overwrites the whole file, command=str_replace replaces one exact match, command=insert adds a new line.",
		func(_ context.Context, in codeEditIn) (llm.Result, error) {
			if a.codeedit == nil {
				return llm.Result{}, errRepoNotCloned
			}
			if !a.repoMu.TryLock() {
				return llm.Result{}, errRepoBusy
			}
			defer a.repoMu.Unlock()

			switch in.Command {
			case "write":
				if err := a.codeedit.Write(in.Path, in.FileText); err != nil {
					return llm.Result{}, err
				}
				return llm.Result{Content: "file written"}, nil
			case "str_replace":
				n, err := a.codeedit.Replace(in.Path, in.OldStr, in.NewStr, false)
				if err != nil {
					return llm.Result{}, err
				}
				return llm.Result{Content: fmt.Sprintf("%d replacement made", n)}, nil
			case "insert":
				if err := a.codeedit.Insert(in.Path, in.InsertLine, in.NewStr); err != nil {
					return llm.Result{}, err
				}
				return llm.Result{Content: "line inserted"}, nil
			default:
				return llm.Result{}, fmt.Errorf("unknown command %q, must be write, str_replace, or insert", in.Command)
			}
		})
}
