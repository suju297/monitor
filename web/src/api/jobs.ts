import { api as legacyApi } from './legacy'

export type { JobApplicationStatusUpdateResponse, JobsProgressResponse, JobsResponse } from './legacy'

export const jobsApi = {
  getJobs: legacyApi.getJobs,
  getJobsProgress: legacyApi.getJobsProgress,
  updateJobApplicationStatus: legacyApi.updateJobApplicationStatus,
}

