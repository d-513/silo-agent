import type { ChatRename } from "./useChatList";

// The in-place title field of a chat being renamed: Enter saves, Escape leaves,
// leaving the field saves too.
export function ChatTitleInput({ rename, cid, className }: { rename: ChatRename; cid: string; className: string }) {
  return (
    <input
      autoFocus
      className={className}
      value={rename.title}
      onChange={(e) => rename.setTitle(e.target.value)}
      onBlur={() => rename.blur(cid)}
      onKeyDown={(e) => {
        if (e.key === "Enter") {
          e.preventDefault();
          rename.commit(cid);
        }
        if (e.key === "Escape") rename.cancel();
      }}
    />
  );
}
