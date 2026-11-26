// The dsh build keeps these state shapes in src/host, beside the log folding
// code. The port only needs the shapes: the projection is assembled in Go, so
// the folding machinery is not carried over. Names and fields stay identical so
// the ported components run unchanged.
import type {
  ActivityDay,
  Category,
  CommandRecord,
  ContextEventRecord,
  FileOpRecord,
  RequestRecord,
  SessionCostUsage,
  SlimRequestRecord,
  SurfaceNode,
  SystemPromptNode,
  TimingSpan,
  TimingTotals,
  CommandStats,
} from './types'

export interface ToolStart {
  start: number
  day: string
  name?: string
  transport?: true
}

export interface ActivityState {
  days: Record<string, ActivityDay>
  model?: string
  provider?: string
  stepStart?: number
  lastTurn?: number
  toolStarts?: Record<string, ToolStart | undefined>
  skillCalls?: string[]
}

export interface HeaderTool {
  name: string
  tokens: number
  plugin?: string
}

export interface StoredHeaderRecord {
  seq: number
  time: number
  system?: string
  systemTokens?: number
  tools: HeaderTool[]
}

export interface HeadersState {
  headers: StoredHeaderRecord[]
}

export interface CommandStateRecord extends CommandRecord {
  call?: string
}

export interface TimelineEvent {
  type: string
  seq: number
  time: number
  data?: Record<string, unknown>
  surfaceOp?: unknown
}

export interface AgentAlias {
  catalogName?: string
  memberName?: string
  catalogLabel?: string
}

export type WorkflowTodoStatus = 'pending' | 'in_progress' | 'completed'

export type WorkflowTeamTaskStatus = WorkflowTodoStatus | 'deleted'

export interface CumulativeTotals {
  turns: number
  steps: number
  events: number
  injects: number
  compactions: number
  prunes: number
  modelSwitches: number
  skills: Record<string, true>
  lastTurn?: number
}

export interface TimelineState {
  surface: SurfaceNode[]
  sums: Record<Category, number>
  assistantReasoning: number
  assistantAnswerTokens: number
  assistantCallTokens: number
  systemTokens: number
  systems?: SystemPromptNode[]
  toolsTokens: number
  model?: string
  provider?: string
  reasoningEffort?: string
  lastModel?: string
  contextWindow?: number
  maxOutputTokens?: number
  requests: SlimRequestRecord[]
  turnRuns?: number
  totals?: CumulativeTotals
  events: ContextEventRecord[]
  archived: SurfaceNode[]
  cost?: SessionCostUsage
  generatedReasoning?: number
  generatedAnswerTokens?: number
  generatedCallTokens?: number
  humanInputs?: number
  answers?: number
  lastUser?: string
  archiveFloor?: number
  detailRev?: number
  timing?: TimingTotals
  stepStart?: { time: number; firstToken?: number }
  stepWaits?: { start: number; end: number }[]
  stepApprovals?: Record<string, number>
  stepRequestSeq?: number
  stepBeats?: number[]
  stepBeatsFull?: boolean
  spans: TimingSpan[]
  stepSpans?: TimingSpan[]
  callNames: Record<string, { name: string; start: number; argsRaw?: string; transport?: true }>
  pendingShadowedSeqs?: number[]
  pendingShadowEventSeq?: number
  fileOps: FileOpRecord[]
  fileOpsFloor?: number
  commands: CommandStateRecord[]
  commandsFloor?: number
  commandStats?: CommandStats
  toolTally?: Record<string, number>
  programTally?: Record<string, number>
  commandCalls?: string[]
  commandCallsOverflow?: boolean
  agents?: Record<string, AgentAlias>
  todoItems?: Record<string, WorkflowTodoStatus>
  teamTasks?: Record<string, WorkflowTeamTaskStatus>
  pendingCodeCmds?: Record<string, CommandStateRecord[]>
  pendingCodeOps?: Record<string, FileOpRecord[]>
  pendingCodeStarts?: Record<string, number>
}

export type { RequestRecord }
