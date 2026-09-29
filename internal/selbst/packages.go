package selbst

var packages = map[string]string{
	"agent":       "rick's turn loop: builds prompts, calls the model, runs tools (web, memory, discord, sys, code), handles mentions and ambient interjections, runs sys_task background tasks via the task runner",
	"ambient":     "decides when rick butts in unprompted: cheap gate (activity window, cooldown, daily cap) then an llm classifier that scores the burst",
	"archive":     "long-term memory: chunks and embeds chat history, hybrid recall retrieval, affinity scores and their llm scorer, typing theatre",
	"brave":       "brave search api: web search, page fetch and image search for web_search, web_fetch and web_media",
	"codeedit":    "jailed file read/write/replace/insert inside rick's source checkout for code_read and code_edit",
	"config":      "env-based configuration loaded once at startup",
	"discord":     "disgo gateway bot: event handlers, slash commands, member and presence cache, archive ingestion, backfill, ambient checks, starts the task runner, deploy and interrupted-task reports on boot",
	"giphy":       "giphy gif search for the web_media tool",
	"llm":         "openrouter client: chat completions with tools and prompt caching, embeddings, tool schema derivation",
	"pgtest":      "test-only postgres harness: one pgvector testcontainer per test binary, truncates tables per test, skips without docker",
	"runtimehome": "runtime dir in the work dir: daily jsonl logs, per-boot crash files, deploy journal (ship, boot, reported), readers behind sys_logs, group permissions",
	"sandbox":     "manifest of native alpine tools installed for sys_shell (tools.txt)",
	"selbst":      "rick's self-knowledge: build and runtime facts for the <selbst> block, this package map, live vitals, deploy status and the post-restart deploy report",
	"selfcode":    "code_ship helpers: postgres snapshots and verifying pending migrations against a scratch database",
	"shell":       "sys_shell executor: runs bash as the unprivileged shell user with a timeout, output cap and process-group kill",
	"store":       "postgres via gorm plus goose migrations: messages, chunks, embeddings, quotes, tasks, affinity, traces, token usage",
	"tracer":      "records each turn (prompt, tools, tokens, response); keeps successes in memory, persists failures",
	"web":         "http server: discord oauth login, quote pages, debug trace viewer, goroutine leak profile",
}
