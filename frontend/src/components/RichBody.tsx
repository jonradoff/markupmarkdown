// Renders a comment / reply body, linkifying @mention tokens to the
// referenced GitHub profile. Keeps newlines.

const MENTION = /(^|[^\w])@([a-zA-Z0-9](?:[a-zA-Z0-9-]{0,38}))/g;

interface Props {
  body: string;
  highlightLogin?: string;
}

export default function RichBody({ body, highlightLogin }: Props) {
  const out: React.ReactNode[] = [];
  let lastIndex = 0;
  let m: RegExpExecArray | null;
  let key = 0;
  MENTION.lastIndex = 0;
  while ((m = MENTION.exec(body)) !== null) {
    const [whole, prefix, login] = m;
    const start = m.index + prefix.length;
    if (start > lastIndex) {
      out.push(body.slice(lastIndex, start));
    }
    const isSelf =
      highlightLogin &&
      highlightLogin.toLowerCase() === login.toLowerCase();
    out.push(
      <a
        key={`m-${key++}`}
        href={`https://github.com/${login}`}
        target="_blank"
        rel="noreferrer"
        className={[
          "rounded px-1 -mx-0.5 font-medium",
          isSelf
            ? "bg-accent text-accent-fg"
            : "bg-accent-soft text-accent",
          "hover:underline",
        ].join(" ")}
      >
        @{login}
      </a>
    );
    lastIndex = m.index + whole.length;
  }
  if (lastIndex < body.length) {
    out.push(body.slice(lastIndex));
  }

  return <span className="whitespace-pre-wrap break-words">{out}</span>;
}
