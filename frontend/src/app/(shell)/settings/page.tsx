export default function SettingsPage() {
  return (
    <div className="flex flex-col gap-4 px-8 py-7">
      <div className="text-[15px] font-semibold">Settings</div>
      <div className="max-w-xl rounded-[10px] border border-border bg-surface p-5 text-sm text-text-muted">
        Cluster connections, GitOps repository, and user preferences will live
        here. Not part of the initial design concept — placeholder for now.
      </div>
    </div>
  );
}
