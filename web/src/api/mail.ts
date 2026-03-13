import { api as legacyApi } from './legacy'

export type {
  CrawlScheduleResponse,
  MailAccount,
  MailAccountActionResponse,
  MailAnalyticsResponse,
  MailConnectResponse,
  MailCountPair,
  MailDailyBucket,
  MailLifecycleApplicationItem,
  MailLifecycleRejectionItem,
  MailMeetingItem,
  MailMessageDetailResponse,
  MailMessagesResponse,
  MailOpenActionItem,
  MailOverviewResponse,
  MailRunStatusResponse,
  MailTriageUpdateResponse,
  TaskTriggerResponse,
} from './legacy'

export const mailApi = {
  getMailOverview: legacyApi.getMailOverview,
  getMailAnalytics: legacyApi.getMailAnalytics,
  getMailAccounts: legacyApi.getMailAccounts,
  getMailMessages: legacyApi.getMailMessages,
  getMailMessageDetail: legacyApi.getMailMessageDetail,
  updateMailTriage: legacyApi.updateMailTriage,
  startMailConnect: legacyApi.startMailConnect,
  disconnectMailAccount: legacyApi.disconnectMailAccount,
  triggerMailRun: legacyApi.triggerMailRun,
  getMailRunStatus: legacyApi.getMailRunStatus,
  getMailSchedule: legacyApi.getMailSchedule,
  updateMailSchedule: legacyApi.updateMailSchedule,
}

