import { useCallback, useEffect, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import { api } from "../api";
import type { NotificationItem } from "../types";
import { useAuth } from "../auth";
import { formatRelative } from "../utils/format";

export default function NotificationBell() {
  const { user } = useAuth();
  const navigate = useNavigate();
  const [open, setOpen] = useState(false);
  const [notifications, setNotifications] = useState<NotificationItem[]>([]);
  const [unread, setUnread] = useState(0);
  const menuRef = useRef<HTMLDivElement>(null);

  const refresh = useCallback(async () => {
    if (!user) return;
    try {
      const res = await api.listNotifications();
      setNotifications(res.notifications);
      setUnread(res.unread);
    } catch {
      /* network blip — try again next tick */
    }
  }, [user]);

  // Poll every 45s while the tab is visible, and refresh immediately on focus.
  useEffect(() => {
    if (!user) {
      setNotifications([]);
      setUnread(0);
      return;
    }
    refresh();
    const onFocus = () => refresh();
    window.addEventListener("focus", onFocus);
    const id = window.setInterval(() => {
      if (document.visibilityState === "visible") refresh();
    }, 45_000);
    return () => {
      window.removeEventListener("focus", onFocus);
      window.clearInterval(id);
    };
  }, [user, refresh]);

  // Close the menu on outside click.
  useEffect(() => {
    function onClick(e: MouseEvent) {
      if (menuRef.current && !menuRef.current.contains(e.target as Node)) {
        setOpen(false);
      }
    }
    document.addEventListener("mousedown", onClick);
    return () => document.removeEventListener("mousedown", onClick);
  }, []);

  if (!user) return null;

  async function openNotification(n: NotificationItem) {
    setOpen(false);
    if (!n.readAt) {
      try {
        await api.markNotificationRead(n.id);
      } catch {}
      setUnread((u) => Math.max(0, u - 1));
      setNotifications((list) =>
        list.map((x) =>
          x.id === n.id ? { ...x, readAt: new Date().toISOString() } : x
        )
      );
    }
    navigate(`/d/${n.documentId}?comment=${n.commentId}`);
  }

  async function markAllRead() {
    try {
      await api.markAllNotificationsRead();
    } catch {}
    setUnread(0);
    const now = new Date().toISOString();
    setNotifications((list) =>
      list.map((x) => (x.readAt ? x : { ...x, readAt: now }))
    );
  }

  return (
    <div className="relative" ref={menuRef}>
      <button
        onClick={() => {
          setOpen((o) => !o);
          if (!open) refresh();
        }}
        title="Notifications"
        aria-label={
          unread > 0 ? `${unread} unread notifications` : "Notifications"
        }
        className="w-8 h-8 rounded-md flex items-center justify-center text-muted hover:text-ink hover:bg-soft relative"
      >
        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
          <path d="M18 8A6 6 0 0 0 6 8c0 7-3 9-3 9h18s-3-2-3-9" />
          <path d="M13.73 21a2 2 0 0 1-3.46 0" />
        </svg>
        {unread > 0 && (
          <span className="absolute top-0 right-0 -mt-1 -mr-1 min-w-[1.1rem] h-[1.1rem] px-1 rounded-full bg-danger text-white text-[10px] font-semibold flex items-center justify-center tabular-nums">
            {unread > 99 ? "99+" : unread}
          </span>
        )}
      </button>

      {open && (
        <div className="absolute right-0 mt-2 w-96 max-h-[28rem] bg-card border border-rule rounded-lg shadow-lg overflow-hidden z-30 flex flex-col">
          <div className="flex items-center justify-between px-3 py-2 border-b border-rule shrink-0">
            <div className="font-medium text-sm">Notifications</div>
            {unread > 0 && (
              <button
                onClick={markAllRead}
                className="text-xs text-accent hover:underline"
              >
                Mark all read
              </button>
            )}
          </div>
          <div className="flex-1 min-h-0 overflow-auto">
            {notifications.length === 0 ? (
              <div className="text-xs text-muted text-center py-8 px-4">
                You're all caught up.
              </div>
            ) : (
              <ul className="divide-y divide-rule">
                {notifications.map((n) => (
                  <li key={n.id}>
                    <button
                      onClick={() => openNotification(n)}
                      className={[
                        "w-full text-left px-3 py-2.5 hover:bg-soft flex items-start gap-2",
                        !n.readAt ? "bg-accent-soft/40" : "",
                      ].join(" ")}
                    >
                      {n.actorAvatarUrl ? (
                        <img
                          src={n.actorAvatarUrl}
                          alt=""
                          className="w-7 h-7 rounded-full bg-soft shrink-0"
                          loading="lazy"
                        />
                      ) : (
                        <span className="w-7 h-7 rounded-full bg-soft shrink-0" />
                      )}
                      <div className="flex-1 min-w-0 text-sm">
                        <div className="text-ink">
                          <strong className="font-medium">{n.actorName}</strong>{" "}
                          {n.kind === "mention" ? "mentioned you in" : "replied in"}{" "}
                          <span className="text-accent">{n.documentTitle}</span>
                        </div>
                        <div className="text-xs text-muted line-clamp-2 mt-0.5">
                          {n.preview}
                        </div>
                        <div className="text-[10px] text-faint mt-0.5">
                          {formatRelative(n.createdAt)}
                        </div>
                      </div>
                      {!n.readAt && (
                        <span className="w-2 h-2 rounded-full bg-accent shrink-0 mt-2" aria-hidden />
                      )}
                    </button>
                  </li>
                ))}
              </ul>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
