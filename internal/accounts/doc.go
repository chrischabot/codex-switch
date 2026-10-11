// Package accounts owns the ChatGPT accounts in the pool: signing them in and
// removing them, keeping their Codex credentials fresh, and watching what
// OpenAI reports about each account's allowance, models and daily token use.
// It makes every call to chatgpt.com and auth.openai.com that Codex Switch
// starts itself; the relay only forwards Codex's own requests.
package accounts
