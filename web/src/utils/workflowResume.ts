const storageKey = (runId: string) => `agent.workflow.resume.${runId}`

export const clearWorkflowResume = (runId: string) => {
  if (!runId) return
  try {
    sessionStorage.removeItem(storageKey(runId))
  } catch {
    // Legacy browser state is best-effort cleanup only.
  }
}
