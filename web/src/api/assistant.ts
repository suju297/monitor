import { api as legacyApi } from './legacy'

export type {
  AssistantConfigResponse,
  AssistantJobsBatchResponse,
  AssistantLearningFamily,
  AssistantLearningSummaryResponse,
  AssistantQueueItem,
  AssistantQueueMutationResponse,
  AssistantQueueResponse,
  AssistantRunHistoryResponse,
  AssistantRunStatusResponse,
  AssistantRunsResponse,
  AssistantTraceDetailResponse,
  ObserverRecommendationResponse,
} from './legacy'

export const assistantApi = {
  getObserverRecommendation: legacyApi.getObserverRecommendation,
  startAssistantRun: legacyApi.startAssistantRun,
  getAssistantConfig: legacyApi.getAssistantConfig,
  getAssistantRun: legacyApi.getAssistantRun,
  getAssistantRuns: legacyApi.getAssistantRuns,
  getAssistantHistory: legacyApi.getAssistantHistory,
  getAssistantTrace: legacyApi.getAssistantTrace,
  getAssistantLearningSummary: legacyApi.getAssistantLearningSummary,
  getAssistantQueue: legacyApi.getAssistantQueue,
  getAssistantJobsBatch: legacyApi.getAssistantJobsBatch,
  enqueueAssistantJob: legacyApi.enqueueAssistantJob,
  enqueueAssistantJobsBatch: legacyApi.enqueueAssistantJobsBatch,
  startNextQueuedAssistantRun: legacyApi.startNextQueuedAssistantRun,
  updateAssistantQueueResume: legacyApi.updateAssistantQueueResume,
}

