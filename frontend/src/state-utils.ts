export type ErrorState = {
  actionError: string;
  refreshError: string;
};

export type ErrorAction =
  | { type: "action-started" }
  | { type: "action-failed"; message: string }
  | { type: "action-dismissed" }
  | { type: "poll-result"; message: string };

export function errorReducer(state: ErrorState, action: ErrorAction): ErrorState {
  switch (action.type) {
    case "action-started":
      return { ...state, actionError: "" };
    case "action-failed":
      return { ...state, actionError: action.message };
    case "action-dismissed":
      return { ...state, actionError: "" };
    case "poll-result":
      return { ...state, refreshError: action.message };
  }
}

export function isStale(lastUpdated: number | null, now = Date.now(), thresholdMs = 12_000): boolean {
  return lastUpdated !== null && now - lastUpdated > thresholdMs;
}

export function pollInterval(state: string): number {
  return state === "running" ? 1_000 : 4_000;
}

export type SampleWithTimestamp = { at: number };

export function appendRollingSample<T extends SampleWithTimestamp>(samples: T[], next: T, windowMs = 60_000, maximum = 60): T[] {
  const cutoff = next.at - windowMs;
  return [...samples.filter((sample) => sample.at >= cutoff), next].slice(-maximum);
}

export function withTimeout<T>(operation: PromiseLike<T>, timeoutMs: number, label: string): Promise<T> {
  const cancellable = operation as PromiseLike<T> & { cancel?: () => void };
  let timer: ReturnType<typeof setTimeout> | undefined;
  const timeout = new Promise<T>((_, reject) => {
    timer = setTimeout(() => {
      try {
        cancellable.cancel?.();
      } catch {
        // The request is already timing out; preserve the actionable timeout.
      }
      reject(new Error(`${label} timed out. Check the tunnel state before retrying.`));
    }, timeoutMs);
  });
  return Promise.race([Promise.resolve(operation), timeout]).finally(() => {
    if (timer !== undefined) clearTimeout(timer);
  });
}
