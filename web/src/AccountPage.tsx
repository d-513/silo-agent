export function AccountPage({ email }: { email: string }) {
  return (
    <div className="silo-page silo-page-sm">
      <h1 className="mb-2 text-title">Account</h1>
      <p className="mb-6 text-ink-2">Your sign-in. More settings later.</p>
      <div className="mb-1 text-[12px] font-medium text-ink-3">Email</div>
      <div className="rounded-sm shadow-[inset_0_0_0_1px_var(--color-line-strong)] bg-surface px-3 py-2">{email}</div>
    </div>
  );
}
