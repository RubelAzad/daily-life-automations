"use client";

import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { useCallback, useEffect, useMemo, useRef, useState, type FormEvent, type KeyboardEvent } from "react";
import AppHeader, { ErrorBanner } from "@/components/AppHeader";
import {
  approveAIAction,
  createConversation,
  deleteConversation,
  getAIStatus,
  getConversation,
  listAIActions,
  listConversations,
  rejectAIAction,
  sendChatMessage,
  type AIAction,
  type AIStatus,
  type ChatBlock,
  type ChatMessage,
  type Conversation,
  type ToolResult,
} from "@/lib/api";
import { errorMessage } from "@/lib/errors";
import { formatDue, timeAgo } from "@/lib/format";
import { useRequireUser } from "@/lib/useRequireUser";

const MAX_CHARS = 4000;

const SUGGESTIONS = [
  "What's on my plate today?",
  "Remind me to call Mom tomorrow at 6 pm",
  "Move my overdue tasks to tomorrow",
  "Add a dentist appointment next Tuesday at 10:00 for an hour",
];

export default function ChatView() {
  const { user, error: authError } = useRequireUser();
  const router = useRouter();
  const params = useSearchParams();
  const selected = Number(params.get("c")) || null;

  const [status, setStatus] = useState<AIStatus | null>(null);
  const [chats, setChats] = useState<Conversation[] | null>(null);
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [actions, setActions] = useState<Map<number, AIAction>>(new Map());
  // The chat whose messages are shown; loading while it differs from ?c=.
  const [loadedId, setLoadedId] = useState<number | null>(null);
  const [sending, setSending] = useState(false);
  const [deciding, setDeciding] = useState<number | null>(null);
  const [text, setText] = useState("");
  const [error, setError] = useState<string | null>(null);
  const bottom = useRef<HTMLDivElement>(null);

  const showError = useCallback((err: unknown) => setError(errorMessage(err)), []);

  const refreshChats = useCallback(() => {
    listConversations({ per_page: 50 })
      .then((r) => setChats(r.data))
      .catch(showError);
  }, [showError]);

  useEffect(() => {
    if (!user) return;
    getAIStatus().then(setStatus).catch(showError);
    refreshChats();
  }, [user, refreshChats, showError]);

  // Load the selected chat and the live status of its actions.
  useEffect(() => {
    if (!user) return;
    if (!selected) return; // a new chat; open(null) cleared the thread
    let cancelled = false;
    Promise.all([getConversation(selected), listAIActions({ conversation_id: selected, kind: "tool", per_page: 100 })])
      .then(([conv, acts]) => {
        if (cancelled) return;
        setMessages(conv.messages ?? []);
        setActions(new Map(acts.data.map((a) => [a.id, a])));
        setLoadedId(selected);
      })
      .catch((err) => {
        if (cancelled) return;
        showError(err);
        setLoadedId(selected);
      });
    return () => {
      cancelled = true;
    };
  }, [user, selected, showError]);

  useEffect(() => {
    bottom.current?.scrollIntoView({ block: "end" });
  }, [messages, sending]);

  const results = useMemo(() => toolResults(messages), [messages]);

  function open(id: number | null) {
    if (!id) {
      setMessages([]);
      setActions(new Map());
    }
    router.replace(id ? `/ai?c=${id}` : "/ai");
  }

  async function send(raw: string) {
    const message = raw.trim();
    if (!message || sending) return;
    setSending(true);
    setError(null);
    setText("");
    const pending: ChatMessage = { id: -Date.now(), role: "user", content: [{ type: "text", text: message }], created_at: new Date().toISOString() };
    setMessages((m) => [...m, pending]);
    let chatId = selected;
    try {
      if (!chatId) {
        chatId = (await createConversation()).id;
      }
      const turn = await sendChatMessage(chatId, message);
      setMessages((m) => [...m.filter((x) => x.id !== pending.id), ...turn.messages]);
      setActions((prev) => {
        const next = new Map(prev);
        for (const a of turn.actions) next.set(a.id, a);
        return next;
      });
      if (chatId !== selected) open(chatId);
      refreshChats();
    } catch (err) {
      setMessages((m) => m.filter((x) => x.id !== pending.id));
      setText(message); // keep it so it can be resent
      showError(err);
      if (chatId && chatId !== selected) {
        open(chatId);
        refreshChats();
      }
    } finally {
      setSending(false);
    }
  }

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    void send(text);
  }

  function onKeyDown(event: KeyboardEvent<HTMLTextAreaElement>) {
    if (event.key === "Enter" && !event.shiftKey && !event.nativeEvent.isComposing) {
      event.preventDefault();
      void send(text);
    }
  }

  async function decide(id: number, approve: boolean) {
    setDeciding(id);
    try {
      const a = approve ? await approveAIAction(id) : await rejectAIAction(id);
      setActions((prev) => new Map(prev).set(a.id, a));
      if (a.status === "failed") setError(`Couldn't do it: ${a.error ?? "unknown error"}`);
    } catch (err) {
      showError(err);
    } finally {
      setDeciding(null);
    }
  }

  async function onDelete() {
    if (!selected || !window.confirm("Delete this chat? The activity log keeps a record of what the assistant did.")) return;
    try {
      await deleteConversation(selected);
      open(null);
      refreshChats();
    } catch (err) {
      showError(err);
    }
  }

  if (!user) return <p className="p-6 text-zinc-500">{authError ?? "Loading…"}</p>;

  const enabled = status?.enabled ?? false;
  const loading = selected !== null && loadedId !== selected && !sending;
  const current = chats?.find((c) => c.id === selected);
  const visible = messages.filter((m) => m.role === "assistant" || m.content.some((b) => b.type === "text"));

  return (
    <div className="mx-auto flex w-full max-w-6xl flex-col gap-4 px-4 py-6">
      <AppHeader user={user} />
      <ErrorBanner error={error} onDismiss={() => setError(null)} />

      <div className="grid gap-4 md:grid-cols-[14rem_1fr]">
        <aside className="flex flex-col gap-2" aria-label="Chats">
          <button
            type="button"
            onClick={() => open(null)}
            className="rounded-md bg-zinc-900 px-3 py-1.5 text-sm font-medium text-white dark:bg-zinc-100 dark:text-zinc-900"
          >
            New chat
          </button>
          <nav className="flex max-h-[60vh] flex-col overflow-y-auto text-sm">
            {chats?.map((c) => (
              <button
                key={c.id}
                type="button"
                onClick={() => open(c.id)}
                aria-current={c.id === selected ? "page" : undefined}
                className="flex flex-col rounded-md px-2 py-1.5 text-left hover:bg-zinc-100 aria-[current=page]:bg-zinc-100 dark:hover:bg-zinc-800 dark:aria-[current=page]:bg-zinc-800"
              >
                <span className="truncate">{c.title}</span>
                <span className="text-xs text-zinc-500">{timeAgo(c.updated_at)}</span>
              </button>
            ))}
            {chats?.length === 0 && <p className="px-2 py-1 text-xs text-zinc-500">No chats yet.</p>}
          </nav>
          <Link href="/ai/activity" className="px-2 text-xs text-zinc-500 underline">
            Activity log &amp; approvals
          </Link>
        </aside>

        <section className="flex min-h-[70vh] flex-col rounded-md border border-zinc-200 dark:border-zinc-800" aria-label="Chat">
          <div className="flex items-center justify-between gap-2 border-b border-zinc-200 px-4 py-2 dark:border-zinc-800">
            <h1 className="truncate text-sm font-medium">{current?.title ?? "New chat"}</h1>
            {selected && (
              <button type="button" onClick={onDelete} className="text-xs text-zinc-500 underline">
                Delete chat
              </button>
            )}
          </div>

          {status && !enabled && (
            <p className="m-4 rounded-md bg-amber-50 px-3 py-2 text-sm text-amber-900 dark:bg-amber-950 dark:text-amber-200">
              The assistant is off: {status.reason}{" "}
              <Link href="/ai/activity" className="underline">
                Details
              </Link>
            </p>
          )}

          <div className="flex flex-1 flex-col gap-4 overflow-y-auto px-4 py-4" aria-live="polite">
            {loading ? (
              <p className="text-sm text-zinc-500">Loading chat…</p>
            ) : visible.length === 0 && !sending ? (
              <div className="m-auto flex max-w-md flex-col items-center gap-3 text-center">
                <p className="text-zinc-600 dark:text-zinc-400">Ask about your day, or tell me what to add, move or remind you about.</p>
                <div className="flex flex-wrap justify-center gap-2">
                  {SUGGESTIONS.map((s) => (
                    <button
                      key={s}
                      type="button"
                      disabled={!enabled}
                      onClick={() => setText(s)}
                      className="rounded-full border border-zinc-300 px-3 py-1 text-xs hover:bg-zinc-100 disabled:opacity-50 dark:border-zinc-700 dark:hover:bg-zinc-800"
                    >
                      {s}
                    </button>
                  ))}
                </div>
              </div>
            ) : (
              visible.map((m) => (
                <MessageView key={m.id} message={m} results={results} actions={actions} deciding={deciding} onDecide={decide} />
              ))
            )}
            {sending && <p className="text-sm text-zinc-500">Thinking…</p>}
            <div ref={bottom} />
          </div>

          <form onSubmit={onSubmit} className="flex flex-col gap-1 border-t border-zinc-200 p-3 dark:border-zinc-800">
            <div className="flex items-end gap-2">
              <textarea
                value={text}
                onChange={(e) => setText(e.target.value)}
                onKeyDown={onKeyDown}
                rows={2}
                maxLength={MAX_CHARS}
                disabled={!enabled || sending}
                placeholder={enabled ? "Message the assistant… (Enter to send, Shift+Enter for a new line)" : "The assistant is off"}
                aria-label="Message"
                className="min-h-10 flex-1 resize-y rounded-md border border-zinc-300 bg-transparent px-3 py-2 text-sm disabled:opacity-60 dark:border-zinc-700"
              />
              <button
                type="submit"
                disabled={!enabled || sending || !text.trim()}
                className="rounded-md bg-zinc-900 px-4 py-2 text-sm font-medium text-white disabled:opacity-50 dark:bg-zinc-100 dark:text-zinc-900"
              >
                Send
              </button>
            </div>
            {text.length > MAX_CHARS - 500 && (
              <span className="text-right text-xs text-zinc-500">
                {text.length}/{MAX_CHARS}
              </span>
            )}
          </form>
        </section>
      </div>
    </div>
  );
}

/** tool_use id → its result, from the tool_result blocks the app sent back. */
function toolResults(messages: ChatMessage[]): Map<string, ToolResult> {
  const out = new Map<string, ToolResult>();
  for (const m of messages) {
    for (const b of m.content) {
      if (b.type !== "tool_result") continue;
      try {
        out.set(b.tool_use_id, JSON.parse(b.content) as ToolResult);
      } catch {
        out.set(b.tool_use_id, { action_id: 0, status: "failed", error: "unreadable result" });
      }
    }
  }
  return out;
}

function MessageView({
  message,
  results,
  actions,
  deciding,
  onDecide,
}: {
  message: ChatMessage;
  results: Map<string, ToolResult>;
  actions: Map<number, AIAction>;
  deciding: number | null;
  onDecide: (id: number, approve: boolean) => void;
}) {
  if (message.role === "user") {
    const text = message.content.filter((b): b is Extract<ChatBlock, { type: "text" }> => b.type === "text").map((b) => b.text).join("\n");
    return (
      <div className="ml-auto max-w-[85%] rounded-2xl rounded-br-sm bg-zinc-900 px-3 py-2 text-sm whitespace-pre-wrap text-white dark:bg-zinc-100 dark:text-zinc-900">
        {text}
      </div>
    );
  }
  return (
    <div className="flex max-w-[90%] flex-col gap-2">
      {message.content.map((b, i) =>
        b.type === "text" ? (
          <p key={i} className="text-sm whitespace-pre-wrap">
            {b.text}
          </p>
        ) : b.type === "tool_use" ? (
          <ActionCard key={b.id} block={b} result={results.get(b.id)} actions={actions} deciding={deciding} onDecide={onDecide} />
        ) : null,
      )}
    </div>
  );
}

const STATUS_TEXT: Record<string, string> = {
  succeeded: "Done",
  failed: "Failed",
  pending_approval: "Needs your approval",
  running: "Running…",
  rejected: "Rejected",
  expired: "Expired",
};

function ActionCard({
  block,
  result,
  actions,
  deciding,
  onDecide,
}: {
  block: Extract<ChatBlock, { type: "tool_use" }>;
  result: ToolResult | undefined;
  actions: Map<number, AIAction>;
  deciding: number | null;
  onDecide: (id: number, approve: boolean) => void;
}) {
  // The live action (it may have been approved since) wins over the result
  // recorded when the assistant asked.
  const live = result ? actions.get(result.action_id) : undefined;
  const status = live?.status ?? result?.status ?? "running";
  const summary = live?.summary ?? result?.summary ?? block.name;
  const err = live?.error ?? result?.error;
  const lookup = (live?.risk ?? "") === "read";

  if (lookup && status === "succeeded") {
    return <p className="text-xs text-zinc-500">↳ {summary}</p>;
  }
  const tone =
    status === "pending_approval"
      ? "border-amber-300 bg-amber-50 dark:border-amber-800 dark:bg-amber-950"
      : status === "failed"
        ? "border-red-300 dark:border-red-900"
        : "border-zinc-200 dark:border-zinc-800";
  return (
    <div className={`flex flex-col gap-2 rounded-md border px-3 py-2 text-sm ${tone}`}>
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <span className="wrap-break-word">{summary}</span>
        <span className="text-xs text-zinc-500">{STATUS_TEXT[status] ?? status}</span>
      </div>
      {err && <span className="text-xs text-red-600 dark:text-red-400">{err}</span>}
      {status === "pending_approval" && result && (
        <div className="flex flex-wrap items-center gap-2">
          <button
            type="button"
            disabled={deciding !== null}
            onClick={() => onDecide(result.action_id, true)}
            className="rounded-md bg-zinc-900 px-3 py-1 text-xs font-medium text-white disabled:opacity-50 dark:bg-zinc-100 dark:text-zinc-900"
          >
            Approve
          </button>
          <button
            type="button"
            disabled={deciding !== null}
            onClick={() => onDecide(result.action_id, false)}
            className="rounded-md border border-zinc-300 px-3 py-1 text-xs disabled:opacity-50 dark:border-zinc-700"
          >
            Reject
          </button>
          {live?.expires_at && <span className="text-xs text-zinc-500">Expires {formatDue(live.expires_at).toLowerCase()}</span>}
        </div>
      )}
    </div>
  );
}
