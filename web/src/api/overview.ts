import { api as legacyApi } from './legacy'

export type { CrawlScheduleResponse, OverviewResponse } from './legacy'

export const overviewApi = {
  getOverview: legacyApi.getOverview,
  getCrawlSchedule: legacyApi.getCrawlSchedule,
  updateCrawlSchedule: legacyApi.updateCrawlSchedule,
  triggerRun: legacyApi.triggerRun,
}

