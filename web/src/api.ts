import { assistantApi } from '@/api/assistant'
import { DEFAULT_OBSERVER_BASE_URL, OBSERVER_BASE_URL_STORAGE_KEY, loadStoredObserverBaseUrl } from '@/api/client'
import { jobsApi } from '@/api/jobs'
import { mailApi } from '@/api/mail'
import { overviewApi } from '@/api/overview'

export { DEFAULT_OBSERVER_BASE_URL, OBSERVER_BASE_URL_STORAGE_KEY, loadStoredObserverBaseUrl }
export type { AssistantConfigResponse, AssistantJobsBatchResponse, AssistantLearningFamily, AssistantLearningSummaryResponse, AssistantQueueItem, AssistantQueueMutationResponse, AssistantQueueResponse, AssistantRunHistoryResponse, AssistantRunStatusResponse, AssistantRunsResponse, AssistantTraceDetailResponse, ObserverRecommendationResponse } from '@/api/assistant'
export type { JobApplicationStatusUpdateResponse, JobsProgressResponse, JobsResponse } from '@/api/jobs'
export type { CrawlScheduleResponse, MailAccount, MailAccountActionResponse, MailAnalyticsResponse, MailConnectResponse, MailCountPair, MailDailyBucket, MailLifecycleApplicationItem, MailLifecycleRejectionItem, MailMeetingItem, MailMessageDetailResponse, MailMessagesResponse, MailOpenActionItem, MailOverviewResponse, MailRunStatusResponse, MailTriageUpdateResponse, TaskTriggerResponse } from '@/api/mail'
export type { OverviewResponse } from '@/api/overview'

export const api = {
  ...overviewApi,
  ...jobsApi,
  ...mailApi,
  ...assistantApi,
}

