// 聊天區共用的額度自動繼續通知。不擋操作；取消鈕只給擁有者。
function QuotaResumeBanner({ resume, isOwner, connected, onCancel }) {
  const view = quotaResumeBannerDescriptor({ resume, isOwner, connected });
  if (!view) return null;
  return (
    <div
      role={view.role}
      aria-live={view.live}
      className="shrink-0 mx-3 sm:mx-7 mb-2 flex flex-wrap items-center gap-2 rounded-lg border border-ra-line bg-ra-card px-2 py-1.5 text-xs text-ra-t2"
    >
      <span className="min-w-0 flex-1">{view.text}</span>
      {view.button ? (
        <button
          type={view.button.type}
          disabled={view.button.disabled}
          onClick={onCancel}
          className="shrink-0 px-2 py-0.5 rounded bg-[oklch(0.62_0.19_275)] hover:brightness-110 text-white disabled:opacity-40"
        >
          {view.button.name}
        </button>
      ) : null}
    </div>
  );
}
