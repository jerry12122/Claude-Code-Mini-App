/**
 * 新建 session 表單的共用狀態與邏輯。
 * SessionView（建立表單）與 ForwardModal（轉發時新建會話）原本各自維護一份幾乎相同的
 * newName/newDir/newAgent/newMode/newCliExtra/newModel 狀態與 payload 組裝邏輯，這裡收成一份。
 * 各自的 JSX／版面仍分開維護（樣式不同），只共用「狀態 + 送出 payload」這塊。
 */
/** 新建會話不再提供 kiro（--no-interactive），改走 kiroacp。既有 kiro session 仍可執行。 */
function agentTypeForCreate(agentType) {
  return agentType === 'kiro' ? 'kiroacp' : (agentType || 'claude');
}

function useNewSessionForm(defaults = {}) {
  const [name, setName]         = useState(defaults.name || '');
  const [workDir, setWorkDir]   = useState(defaults.workDir || '');
  const [agent, setAgentRaw]    = useState(agentTypeForCreate(defaults.agent));
  const [mode, setMode]         = useState(defaults.mode || 'default');
  const [cliExtra, setCliExtra] = useState(defaults.cliExtra || '');
  const [model, setModel]       = useState(defaults.model || '');
  const [effort, setEffort]     = useState(defaults.effort || '');

  /** 切換 agent 時，權限模式要跟著正規化（例如切到 codex 就不能是 acceptEdits）；
   * model／自訂 CLI 引數／effort 是各供應商專屬格式或不支援（如 Cursor 無 effort），
   * 跨供應商不相容，一併清空避免帶著舊設定送出。 */
  const setAgent = (next) => {
    const a = agentTypeForCreate(next);
    if (a !== agent) {
      setModel('');
      setCliExtra('');
      setEffort('');
    }
    setAgentRaw(a);
    setMode((prev) => normalizePermMode(a, prev));
  };

  const reset = (overrides = {}) => {
    const a = agentTypeForCreate(overrides.agent);
    setName(overrides.name || '');
    setWorkDir(overrides.workDir || '');
    setAgentRaw(a);
    setMode(overrides.mode || 'default');
    setCliExtra('');
    setModel('');
    setEffort('');
  };

  /** 依既有 session 的設定回填表單（ForwardModal「複製自」用） */
  const applyFromSession = (s) => {
    const a = agentTypeForCreate(s.agent_type);
    setAgentRaw(a);
    setWorkDir(s.work_dir || '');
    setMode(normalizePermMode(a, s.permission_mode || 'default'));
    const parts = Array.isArray(s.cli_extra_args) ? s.cli_extra_args : [];
    setModel(extractModelFromCliArgs(parts));
    setCliExtra(cliArgsWithoutModel(parts).join('\n'));
    setEffort(a === 'cursor' ? '' : (s.effort || ''));
  };

  const buildCreatePayload = (extra = {}) => {
    const cliMerged = mergeModelIntoCliExtraLines(cliExtra, model);
    return {
      name: name.trim(),
      work_dir: workDir.trim(),
      permission_mode: mode,
      agent_type: agent,
      cli_extra_args: parseCliExtraArgs(cliMerged),
      effort: agent === 'cursor' ? '' : effort,
      ...extra,
    };
  };

  return {
    name, setName,
    workDir, setWorkDir,
    agent, setAgent,
    mode, setMode,
    cliExtra, setCliExtra,
    model, setModel,
    effort, setEffort,
    reset,
    applyFromSession,
    buildCreatePayload,
  };
}
