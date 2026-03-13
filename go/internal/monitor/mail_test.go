package monitor

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseInternetMessageExtractsBodiesAndCalendar(t *testing.T) {
	t.Parallel()

	raw := strings.Join([]string{
		"From: Recruiter <recruiter@example.com>",
		"To: Candidate <candidate@example.com>",
		"Subject: Interview invite",
		"Date: Mon, 09 Mar 2026 10:15:00 -0400",
		"Message-ID: <invite-1@example.com>",
		`Content-Type: multipart/alternative; boundary="mail-boundary"`,
		"",
		"--mail-boundary",
		"Content-Type: text/plain; charset=utf-8",
		"",
		"Hello there. We would like to schedule your interview.",
		"--mail-boundary",
		"Content-Type: text/html; charset=utf-8",
		"",
		"<html><body><p>Hello there.</p><script>alert('x')</script></body></html>",
		"--mail-boundary",
		"Content-Type: text/calendar; charset=utf-8",
		"",
		"BEGIN:VCALENDAR",
		"BEGIN:VEVENT",
		"DTSTART:20260310T180000Z",
		"DTEND:20260310T183000Z",
		"LOCATION:Zoom",
		"ORGANIZER:mailto:recruiter@example.com",
		"END:VEVENT",
		"END:VCALENDAR",
		"--mail-boundary--",
		"",
	}, "\r\n")

	parsed, err := parseInternetMessage([]byte(raw))
	if err != nil {
		t.Fatalf("parseInternetMessage error: %v", err)
	}
	if parsed.Subject != "Interview invite" {
		t.Fatalf("subject = %q, want %q", parsed.Subject, "Interview invite")
	}
	if !strings.Contains(parsed.TextBody, "schedule your interview") {
		t.Fatalf("text body missing expected content: %q", parsed.TextBody)
	}
	if strings.Contains(strings.ToLower(parsed.HTMLBody), "<script") {
		t.Fatalf("html body still contains script tag: %q", parsed.HTMLBody)
	}
	if !strings.Contains(parsed.Calendar, "DTSTART:20260310T180000Z") {
		t.Fatalf("calendar body missing DTSTART: %q", parsed.Calendar)
	}
}

func TestParseInternetMessagePrefersAttachedRFC822Message(t *testing.T) {
	t.Parallel()

	inner := strings.Join([]string{
		"From: OpenAI Careers <noreply@openai.com>",
		"To: Candidate <candidate@example.com>",
		"Subject: Application received for Platform Engineer",
		"Date: Tue, 10 Mar 2026 09:30:00 -0400",
		"Message-ID: <inner-message@example.com>",
		"Content-Type: text/plain; charset=utf-8",
		"",
		"Thank you for applying to the Platform Engineer role at OpenAI.",
	}, "\r\n")
	raw := strings.Join([]string{
		"From: Candidate <candidate@gmail.com>",
		"To: Inbox <jobs@example.com>",
		"Subject: Fwd: Application received",
		`Content-Type: multipart/mixed; boundary="outer-boundary"`,
		"",
		"--outer-boundary",
		"Content-Type: text/plain; charset=utf-8",
		"",
		"Forwarded message attached. Wrapper text should not win.",
		"--outer-boundary",
		"Content-Type: message/rfc822",
		"",
		inner,
		"--outer-boundary--",
		"",
	}, "\r\n")

	parsed, err := parseInternetMessage([]byte(raw))
	if err != nil {
		t.Fatalf("parseInternetMessage error: %v", err)
	}
	if parsed.Subject != "Application received for Platform Engineer" {
		t.Fatalf("subject = %q", parsed.Subject)
	}
	if parsed.MessageID != "<inner-message@example.com>" {
		t.Fatalf("message id = %q", parsed.MessageID)
	}
	if parsed.From.Email != "noreply@openai.com" {
		t.Fatalf("from email = %q", parsed.From.Email)
	}
	if !strings.Contains(parsed.TextBody, "Platform Engineer role at OpenAI") {
		t.Fatalf("text body = %q", parsed.TextBody)
	}
	if strings.Contains(parsed.TextBody, "Wrapper text should not win") {
		t.Fatalf("wrapper text leaked into canonical body: %q", parsed.TextBody)
	}
}

func TestParseInternetMessageUnwrapsAttachedEMLFile(t *testing.T) {
	t.Parallel()

	inner := strings.Join([]string{
		"From: Recruiting <recruiting@example.com>",
		"To: Candidate <candidate@example.com>",
		"Subject: Interview scheduled",
		"Date: Wed, 11 Mar 2026 10:15:00 -0400",
		"Message-ID: <invite-message@example.com>",
		`Content-Type: multipart/alternative; boundary="inner-boundary"`,
		"",
		"--inner-boundary",
		"Content-Type: text/plain; charset=utf-8",
		"",
		"Your interview is scheduled. Invite attached.",
		"--inner-boundary",
		"Content-Type: text/calendar; charset=utf-8",
		"",
		"BEGIN:VCALENDAR",
		"BEGIN:VEVENT",
		"DTSTART:20260312T180000Z",
		"DTEND:20260312T183000Z",
		"LOCATION:Zoom",
		"END:VEVENT",
		"END:VCALENDAR",
		"--inner-boundary--",
		"",
	}, "\r\n")
	encodedInner := base64.StdEncoding.EncodeToString([]byte(inner))
	raw := strings.Join([]string{
		"From: Candidate <candidate@gmail.com>",
		"To: Inbox <jobs@example.com>",
		"Subject: Fwd: Interview scheduled",
		`Content-Type: multipart/mixed; boundary="outer-boundary"`,
		"",
		"--outer-boundary",
		"Content-Type: text/plain; charset=utf-8",
		"",
		"Forwarded as attachment.",
		"--outer-boundary",
		`Content-Type: application/octet-stream; name="forwarded.eml"`,
		"Content-Transfer-Encoding: base64",
		`Content-Disposition: attachment; filename="forwarded.eml"`,
		"",
		encodedInner,
		"--outer-boundary--",
		"",
	}, "\r\n")

	parsed, err := parseInternetMessage([]byte(raw))
	if err != nil {
		t.Fatalf("parseInternetMessage error: %v", err)
	}
	if parsed.Subject != "Interview scheduled" {
		t.Fatalf("subject = %q", parsed.Subject)
	}
	if parsed.MessageID != "<invite-message@example.com>" {
		t.Fatalf("message id = %q", parsed.MessageID)
	}
	if !strings.Contains(parsed.TextBody, "Invite attached") {
		t.Fatalf("text body = %q", parsed.TextBody)
	}
	if !strings.Contains(parsed.Calendar, "DTSTART:20260312T180000Z") {
		t.Fatalf("calendar = %q", parsed.Calendar)
	}
}

func TestClassifyMailMessageRules(t *testing.T) {
	t.Parallel()

	context := mailMatchContext{
		Companies: []string{"OpenAI"},
		Jobs: []mailKnownJob{
			{Company: "OpenAI", Title: "Software Engineer"},
		},
	}

	tests := []struct {
		name       string
		message    MailMessage
		wantEvent  string
		wantImport bool
	}{
		{
			name: "recruiter reply",
			message: MailMessage{
				Subject:      "Re: Software Engineer application",
				Sender:       MailAddress{Name: "Recruiter", Email: "recruiter@openai.com"},
				ToRecipients: []MailAddress{{Email: "candidate@example.com"}},
				BodyText:     "Hi, thanks for applying to the Software Engineer role at OpenAI. Can we chat this week?",
			},
			wantEvent:  mailEventRecruiterReply,
			wantImport: true,
		},
		{
			name: "recruiter outreach",
			message: MailMessage{
				Subject:      "Backend Engineer opportunity at OpenAI",
				Sender:       MailAddress{Name: "Talent Partner", Email: "talent@openai.com"},
				ToRecipients: []MailAddress{{Email: "candidate@example.com"}},
				BodyText:     "Hi Sujendra, I came across your profile and would love to chat about a Backend Engineer role at OpenAI.",
			},
			wantEvent:  mailEventRecruiterOutreach,
			wantImport: true,
		},
		{
			name: "india invited-to-apply mail is ignored",
			message: MailMessage{
				Subject:      "Job | AI/ML Engineer in Bengaluru",
				Sender:       MailAddress{Name: "Naukri", Email: "alerts@naukri.com"},
				ToRecipients: []MailAddress{{Email: "candidate@example.com"}},
				BodyText:     "Job invite from recruiter. Apply now! You've been chosen from a large pool of jobseekers to apply to this job. Hiring for OpenAI AI/ML Engineer role.",
			},
			wantEvent:  mailEventIgnored,
			wantImport: false,
		},
		{
			name: "india interview-themed invite blast is still ignored",
			message: MailMessage{
				Subject:      "Job | F2F Interview - 11th March - Mumbai - AI/ML engineer",
				Sender:       MailAddress{Name: "Natalie Consultants", Email: "sakshi@example@naukri.com"},
				ToRecipients: []MailAddress{{Email: "candidate@example.com"}},
				BodyText:     "Job invite from recruiter. Apply now! You've been chosen from a large pool of jobseekers to apply for this job. Hiring for Foreign MNC. F2F Interview - 11th March - Mumbai - AI/ML engineer.",
			},
			wantEvent:  mailEventIgnored,
			wantImport: false,
		},
		{
			name: "india profile-update mail stays india bucket",
			message: MailMessage{
				Subject:      "Urgent Vacancy - Verify Profile Details",
				Sender:       MailAddress{Name: "Palm HR Consultants", Email: "alerts@jobs.shine.com"},
				ToRecipients: []MailAddress{{Email: "candidate@example.com"}},
				BodyText:     "Dear Sujendra, I reviewed your profile for an urgent opportunity and need a quick confirmation of a few details before I share your CV with the hiring manager.",
			},
			wantEvent:  mailEventIndiaJobMarket,
			wantImport: false,
		},
		{
			name: "local newsletter is ignored",
			message: MailMessage{
				Subject:      "The Orange Line ATE with this ...",
				Sender:       MailAddress{Name: "The B-Side", Email: "thebside@mail.boston.com"},
				ToRecipients: []MailAddress{{Email: "candidate@example.com"}},
				BodyText:     "Read Online. What's on tap today: parking ticket scam alert, things to do, start a membership.",
			},
			wantEvent:  mailEventIgnored,
			wantImport: false,
		},
		{
			name: "generic job board invite stays generic bucket",
			message: MailMessage{
				Subject:      "You are invited to apply to Backend Engineer",
				Sender:       MailAddress{Name: "Talent Network", Email: "jobs@talentnetwork.example"},
				ToRecipients: []MailAddress{{Email: "candidate@example.com"}},
				BodyText:     "You are invited to apply to this job. Apply now to stay ahead in your search for the Backend Engineer role at OpenAI.",
			},
			wantEvent:  mailEventJobBoardInvite,
			wantImport: false,
		},
		{
			name: "interview scheduled",
			message: MailMessage{
				Subject:         "Interview confirmation",
				Sender:          MailAddress{Name: "Recruiting", Email: "recruiting@openai.com"},
				BodyText:        "Your interview is scheduled for tomorrow. Calendar invite attached.",
				HasInvite:       true,
				MeetingStart:    "2026-03-10T18:00:00Z",
				MeetingLocation: "Zoom",
			},
			wantEvent:  mailEventInterviewScheduled,
			wantImport: true,
		},
		{
			name: "application acknowledgement",
			message: MailMessage{
				Subject:  "Application received",
				Sender:   MailAddress{Name: "OpenAI Careers", Email: "noreply@openai.com"},
				BodyText: "Thank you for applying to the Software Engineer role at OpenAI. Your application has been received.",
			},
			wantEvent:  mailEventApplicationAcknowledged,
			wantImport: false,
		},
		{
			name: "verification code ignored",
			message: MailMessage{
				Subject:  "Your one-time verification code",
				Sender:   MailAddress{Name: "OpenAI Careers", Email: "noreply@openai.com"},
				BodyText: "Use this one-time verification code to finish signing in to your OpenAI careers account.",
			},
			wantEvent:  mailEventIgnored,
			wantImport: false,
		},
		{
			name: "identity confirmation code ignored",
			message: MailMessage{
				Subject: "Confirm your identity for job AI Engineer Intern - 31332",
				Sender:  MailAddress{Name: "Chubb Talent Acquisition", Email: "ewgu.fa.sender@workflow.email.uk-london-1.ocs.oraclecloud.com"},
				BodyText: "You're almost finished! To complete your application for the AI Engineer Intern (31332) position, you are required to confirm your identity. " +
					"Confirm your identity using this code: 756616. The code expires in 10 minutes.",
			},
			wantEvent:  mailEventIgnored,
			wantImport: false,
		},
		{
			name: "application acknowledgement wins over account setup noise",
			message: MailMessage{
				Subject:  "Thank you for applying",
				Sender:   MailAddress{Name: "OpenAI Careers", Email: "noreply@openai.com"},
				BodyText: "Thank you for applying to the Software Engineer role at OpenAI. Your application has been received. Create your account to track updates.",
			},
			wantEvent:  mailEventApplicationAcknowledged,
			wantImport: false,
		},
		{
			name: "application acknowledgement with future interview mention stays acknowledgement",
			message: MailMessage{
				Subject: "Your application has been received",
				Sender:  MailAddress{Name: "Cribl Talent Acquisition", Email: "noreply@cribl.io"},
				BodyText: "Thank you for your interest in the Software Engineer, Stream Integrations role at Cribl - your application has been received! " +
					"What to Expect: If your background and experience seem like a good fit for the position, we will contact you soon to schedule a preliminary interview. Stay tuned for more!",
			},
			wantEvent:  mailEventApplicationAcknowledged,
			wantImport: false,
		},
		{
			name: "application acknowledgement beats recruiter template cues",
			message: MailMessage{
				Subject: "Thank you for your application to Veolia North America.",
				Sender:  MailAddress{Name: "Veolia", Email: "notification@smartrecruiters.com"},
				Snippet: "Sujendra Jayant, Thank you for applying for the AI Engineering Intern (SLM) at Veolia North America. " +
					"Your application is currently being carefully reviewed by our hiring team. If your skills and experience align with this opportunity, a member of our team will contact you.",
			},
			wantEvent:  mailEventApplicationAcknowledged,
			wantImport: false,
		},
		{
			name: "application progress reminder beats recruiter outreach cues",
			message: MailMessage{
				Subject: "Complete Your Abbott Job Application",
				Sender:  MailAddress{Name: "Abbott Talent Acquisition", Email: "talentacquisition@careers.abbott.com"},
				Snippet: "Hi Sujendra Jayant, Thank you for your interest in exploring career opportunities at Abbott. " +
					"We noticed that you recently started a job application and encourage you to complete it.",
			},
			wantEvent:  mailEventOtherJobRelated,
			wantImport: false,
		},
		{
			name: "rejection",
			message: MailMessage{
				Subject:  "Update on your application",
				Sender:   MailAddress{Name: "OpenAI Careers", Email: "noreply@openai.com"},
				BodyText: "Unfortunately, we will not be moving forward with other candidates for the Software Engineer role.",
			},
			wantEvent:  mailEventRejection,
			wantImport: true,
		},
		{
			name: "dropbox decided not to move forward is rejection",
			message: MailMessage{
				Subject:  "Update on your candidacy from Dropbox",
				Sender:   MailAddress{Name: "no-reply@dropbox.com", Email: "no-reply@dropbox.com"},
				BodyText: "Thanks so much for your interest in working at Dropbox! We've reviewed your application for the Software Engineer (c) role, but after careful consideration, we've decided not to move forward at this time.",
			},
			wantEvent:  mailEventRejection,
			wantImport: true,
		},
		{
			name: "automated candidacy update subject is rejection even with partial body",
			message: MailMessage{
				Subject:  "Update on your candidacy from Dropbox",
				Sender:   MailAddress{Name: "no-reply@dropbox.com", Email: "no-reply@dropbox.com"},
				Snippet:  "Thanks so much for your interest in working at Dropbox! We've reviewed your application for the Software Engineer (c) role, but after",
				BodyText: "",
			},
			wantEvent:  mailEventRejection,
			wantImport: true,
		},
		{
			name: "applications limit reached is rejection",
			message: MailMessage{
				Subject:  "Update on Your Stripe Applications",
				Sender:   MailAddress{Name: "Stripe Recruiting", Email: "no-reply@stripe.com"},
				BodyText: "Thank you for your interest and the time you've taken to apply to Stripe. We'd like to let you know that you've reached our applications limit. Please feel free to apply for other roles that catch your eye in 30-days.",
			},
			wantEvent:  mailEventRejection,
			wantImport: true,
		},
		{
			name: "permitflow aligned-experience wording is rejection",
			message: MailMessage{
				Subject: "Thanks for your interest in PermitFlow, Sujendra!",
				Sender:  MailAddress{Name: "PermitFlow Recruiting Team", Email: "no-reply@ashbyhq.com"},
				BodyText: "Thank you again for your interest in the Fullstack Software Engineer, New Grad (NYC) opportunity at PermitFlow. " +
					"After reviewing your background, we've decided to move forward with candidates whose experience is more closely aligned with what we're looking for right now.",
			},
			wantEvent:  mailEventRejection,
			wantImport: true,
		},
		{
			name: "newsletter ignored",
			message: MailMessage{
				Subject:  "March engineering newsletter",
				Sender:   MailAddress{Name: "News", Email: "news@example.com"},
				BodyText: "Subscribe to our newsletter and webinar digest.",
			},
			wantEvent:  mailEventIgnored,
			wantImport: false,
		},
		{
			name: "ladders job matching digest stays job board invite",
			message: MailMessage{
				Subject:      "FW: Recruiters want your skills. These jobs won't wait.",
				Sender:       MailAddress{Name: "Sujendra Jayant Gharat", Email: "candidate@example.com"},
				ToRecipients: []MailAddress{{Email: "candidate@example.com"}},
				BodyText: "From: Ladders <jobs@my.theladders.com>\n" +
					"Subject: Recruiters want your skills. These jobs won't wait.\n\n" +
					"Earn top dollar in your field with these high paying jobs.\n" +
					"Software Engineer II / Virtual / Travel / $100K - $150K Remote\n" +
					"Do these jobs match what you're looking for?\n" +
					"You're receiving this email because you signed up on January 05, 2026.\n" +
					"Unsubscribe\n© 2026 Ladders, Inc.",
			},
			wantEvent:  mailEventJobBoardInvite,
			wantImport: false,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			message := test.message
			classifyMailMessageRules(&message, context)
			if message.EventType != test.wantEvent {
				t.Fatalf("event_type = %q, want %q", message.EventType, test.wantEvent)
			}
			if message.Importance != test.wantImport {
				t.Fatalf("importance = %v, want %v", message.Importance, test.wantImport)
			}
		})
	}
}

func TestClassifyMailMessageIgnoresLocalDigestSummaryByBodyCues(t *testing.T) {
	t.Parallel()

	message := MailMessage{
		Subject: "Wednesday city briefing",
		Sender:  MailAddress{Name: "Daily Summary", Email: "summary@example.com"},
		BodyText: `Quick question!
Upcoming local picks for the week.
One last thing before you go.
Thanks for reading and keep up with us for more city coverage.
Let us know below what you think.`,
	}

	classifyMailMessage(&message, mailMatchContext{})

	if message.EventType != mailEventIgnored {
		t.Fatalf("event_type = %q, want %q", message.EventType, mailEventIgnored)
	}
	if message.TriageStatus != mailTriageIgnored {
		t.Fatalf("triage_status = %q, want %q", message.TriageStatus, mailTriageIgnored)
	}
	if message.Importance {
		t.Fatalf("importance = true, want false")
	}
	if !containsFoldedReason(message.Reasons, "Local/newsletter digest ignored") {
		t.Fatalf("expected newsletter ignore reason, got %v", message.Reasons)
	}
}

func TestClassifyMailMessageIgnoresAcademicSpeakerEvent(t *testing.T) {
	t.Parallel()

	context := mailMatchContext{
		Companies: []string{"Microsoft"},
		Jobs: []mailKnownJob{
			{Company: "Microsoft", Title: "Software Engineering"},
		},
	}
	message := MailMessage{
		Subject:      "FW: REMINDER -TODAY: MGEN Speaker Event - What Hiring Managers Really Look For with Kori Rahaim",
		Sender:       MailAddress{Name: "Sujendra Jayant Gharat", Email: "candidate@example.com"},
		ToRecipients: []MailAddress{{Email: "candidate@example.com"}},
		BodyText: "From: Macri, Erin <er.macri@northeastern.edu>\n" +
			"Subject: REMINDER -TODAY: MGEN Speaker Event - What Hiring Managers Really Look For with Kori Rahaim\n\n" +
			"Dear MGEN Students,\nJoin us TODAY for our next MGEN Speaker Series — Voices from the Field, Lessons for the Future!\n" +
			"Location: Behrakis 315 (Boston Campus) and Online (for Network participants)\n" +
			"Register here:\nBoston Students (In-Person) – Fill out form\nNetwork Students (Virtual) – Fill out form\n" +
			"Best,\nMGEN Operations Team\nInformation and Software Engineering\nforms.office.com",
	}

	classifyMailMessageRules(&message, context)

	if message.EventType != mailEventIgnored {
		t.Fatalf("event_type = %q, want %q", message.EventType, mailEventIgnored)
	}
	if message.TriageStatus != mailTriageIgnored {
		t.Fatalf("triage_status = %q, want %q", message.TriageStatus, mailTriageIgnored)
	}
	if message.Importance {
		t.Fatalf("importance = true, want false")
	}
	if message.MatchedCompany != "" {
		t.Fatalf("matched_company = %q, want empty", message.MatchedCompany)
	}
	if message.MatchedJobTitle != "" {
		t.Fatalf("matched_job_title = %q, want empty", message.MatchedJobTitle)
	}
	if !containsFoldedReason(message.Reasons, "Academic or campus event ignored") {
		t.Fatalf("expected academic-event reason, got %v", message.Reasons)
	}
}

func TestClassifyMailMessageSuppressesTrackedMatchesForLaddersDigest(t *testing.T) {
	t.Parallel()

	context := mailMatchContext{
		Companies: []string{"Microsoft"},
		Jobs: []mailKnownJob{
			{Company: "Microsoft", Title: "Software Engineer II"},
		},
	}
	message := MailMessage{
		Subject:      "FW: Recruiters want your skills. These jobs won't wait.",
		Sender:       MailAddress{Name: "Sujendra Jayant Gharat", Email: "candidate@example.com"},
		ToRecipients: []MailAddress{{Email: "candidate@example.com"}},
		BodyText: "From: Ladders <jobs@my.theladders.com>\n" +
			"Earn top dollar in your field with these high paying jobs.\n" +
			"Software Engineer II / Virtual / Travel / $100K - $150K Remote\n" +
			"Do these jobs match what you're looking for?\n" +
			"You're receiving this email because you signed up.\n" +
			"Unsubscribe\n© 2026 Ladders, Inc.",
	}

	classifyMailMessageRules(&message, context)

	if message.EventType != mailEventJobBoardInvite {
		t.Fatalf("event_type = %q, want %q", message.EventType, mailEventJobBoardInvite)
	}
	if message.Importance {
		t.Fatalf("importance = true, want false")
	}
	if message.MatchedCompany != "" {
		t.Fatalf("matched_company = %q, want empty", message.MatchedCompany)
	}
	if message.MatchedJobTitle != "" {
		t.Fatalf("matched_job_title = %q, want empty", message.MatchedJobTitle)
	}
	if !containsFoldedReason(message.Reasons, "Job-board invite template detected") {
		t.Fatalf("expected job-board reason, got %v", message.Reasons)
	}
}

func TestClassifyMailMessageIgnoresGitHubThreadNotification(t *testing.T) {
	t.Parallel()

	context := mailMatchContext{
		Companies: []string{"Openclaw"},
		Jobs: []mailKnownJob{
			{Company: "Openclaw", Title: "Engineer"},
		},
	}
	message := MailMessage{
		Subject:           "Re: [openclaw/openclaw] Agents: force loopback for sessions_spawn gateway calls (PR #29871)",
		Sender:            MailAddress{Name: "openclaw-barnacle[bot]", Email: "notifications@github.com"},
		ToRecipients:      []MailAddress{{Email: "candidate@example.com"}},
		InternetMessageID: "<openclaw/openclaw/pull/29871/c4009408809@github.com>",
		BodyText: "openclaw-barnacle[bot] left a comment (openclaw/openclaw#29871)\n\n" +
			"This pull request has been automatically marked as stale due to inactivity.\n" +
			"Please add updates or it will be closed.\n\n" +
			"Reply to this email directly or view it on GitHub:\n" +
			"https://github.com/openclaw/openclaw/pull/29871#issuecomment-4009408809\n" +
			"You are receiving this because you authored the thread.",
	}

	classifyMailMessageRules(&message, context)

	if message.EventType != mailEventIgnored {
		t.Fatalf("event_type = %q, want %q", message.EventType, mailEventIgnored)
	}
	if message.TriageStatus != mailTriageIgnored {
		t.Fatalf("triage_status = %q, want %q", message.TriageStatus, mailTriageIgnored)
	}
	if message.Importance {
		t.Fatalf("importance = true, want false")
	}
	if message.MatchedCompany != "" {
		t.Fatalf("matched_company = %q, want empty", message.MatchedCompany)
	}
	if message.MatchedJobTitle != "" {
		t.Fatalf("matched_job_title = %q, want empty", message.MatchedJobTitle)
	}
	if containsFoldedReason(message.Reasons, "Direct recruiter reply signal detected") {
		t.Fatalf("expected recruiter-reply reason to be absent, got %v", message.Reasons)
	}
	if !containsFoldedReason(message.Reasons, "GitHub thread notification ignored") {
		t.Fatalf("expected GitHub ignore reason, got %v", message.Reasons)
	}
}

func TestClassifyMailMessageMarksHiringOpsDocumentRequestAsImportantOtherJobRelated(t *testing.T) {
	t.Parallel()

	context := mailMatchContext{
		Companies: []string{"Acme"},
		Jobs: []mailKnownJob{
			{Company: "Acme", Title: "Tax Analyst"},
		},
	}
	message := MailMessage{
		Subject:      "Reg: 2025 Tax filing",
		Sender:       MailAddress{Name: "Kavya", Email: "kavya@example.com"},
		ToRecipients: []MailAddress{{Email: "candidate@example.com"}},
		Snippet:      "Regarding your job application for the Tax Analyst role at Acme.",
		BodyText: "Hi Sujendra,\n\nAs we discussed over the call regarding the documents,\n" +
			"Can you please share the documents for the Tax Analyst role at Acme?",
	}

	classifyMailMessageRules(&message, context)

	if message.EventType != mailEventOtherJobRelated {
		t.Fatalf("event_type = %q, want %q", message.EventType, mailEventOtherJobRelated)
	}
	if !message.Importance {
		t.Fatalf("importance = false, want true")
	}
	if message.Confidence < 0.91 {
		t.Fatalf("confidence = %v, want >= 0.91", message.Confidence)
	}
	if containsFoldedReason(message.Reasons, "Direct recruiter reply signal detected") {
		t.Fatalf("expected recruiter-reply reason to be absent, got %v", message.Reasons)
	}
	if !containsFoldedReason(message.Reasons, "Hiring operations/document request detected") {
		t.Fatalf("expected hiring-ops reason, got %v", message.Reasons)
	}
}

func TestClassifyMailMessageMarksTaxFilerSupportFollowUpAsImportantOtherJobRelated(t *testing.T) {
	t.Parallel()

	message := MailMessage{
		Subject:      "unable to reach you",
		Sender:       MailAddress{Name: "Support .", Email: "support@dollartaxfiler.com"},
		ToRecipients: []MailAddress{{Email: "candidate@example.com"}},
		BodyText: "Dear Sujendra Gharat,\n\nOur team has attempted to contact you by phone through +1(628)229-2999, " +
			"but we were unable to reach you and were directed to voicemail.\n\nPlease contact us at your earliest convenience.\n\nRegards,\nSreevani",
	}

	classifyMailMessageRules(&message, mailMatchContext{})

	if message.EventType != mailEventOtherJobRelated {
		t.Fatalf("event_type = %q, want %q", message.EventType, mailEventOtherJobRelated)
	}
	if !message.Importance {
		t.Fatalf("importance = false, want true")
	}
	if message.Confidence < 0.91 {
		t.Fatalf("confidence = %v, want >= 0.91", message.Confidence)
	}
	if !containsFoldedReason(message.Reasons, "Hiring operations/document request detected") {
		t.Fatalf("expected hiring-ops reason, got %v", message.Reasons)
	}
}

func TestInferMailCompanyAvoidsGenericWordCollision(t *testing.T) {
	t.Parallel()

	context := mailMatchContext{
		Companies: []string{"Box", "OpenAI"},
	}
	message := MailMessage{
		Subject: "Thank you for your interest",
		Sender:  MailAddress{Name: "Talent Acquisition", Email: "noreply@example.com"},
		BodyText: "Thank you for your application. This email box is not monitored. " +
			"We will contact you if there is a fit.",
	}

	if got := inferMailCompany(message, context); got != "" {
		t.Fatalf("inferMailCompany(...) = %q, want empty string", got)
	}
}

func TestMailNeedsSLMForNonInviteInterviewClassification(t *testing.T) {
	t.Parallel()

	nonInviteInterview := MailMessage{
		Subject:         "Interview confirmation",
		Sender:          MailAddress{Name: "Recruiting", Email: "recruiting@openai.com"},
		BodyText:        "Your interview is scheduled for tomorrow.",
		EventType:       mailEventInterviewScheduled,
		MatchedCompany:  "OpenAI",
		MatchedJobTitle: "Software Engineer",
	}
	if !mailNeedsSLM(nonInviteInterview) {
		t.Fatalf("expected non-invite interview classification to be reviewed by SLM")
	}

	inviteInterview := nonInviteInterview
	inviteInterview.HasInvite = true
	if mailNeedsSLM(inviteInterview) {
		t.Fatalf("expected invite-backed interview classification to skip SLM review")
	}
}

func TestMailNeedsSLMSkipsClearRuleBasedRejection(t *testing.T) {
	t.Parallel()

	message := MailMessage{
		Subject:         "Thanks for your interest in PermitFlow, Sujendra!",
		Sender:          MailAddress{Name: "PermitFlow Recruiting Team", Email: "no-reply@ashbyhq.com"},
		BodyText:        "Thank you again for your interest in the Fullstack Software Engineer, New Grad (NYC) opportunity at PermitFlow. After reviewing your background, we've decided to move forward with candidates whose experience is more closely aligned with what we're looking for right now.",
		EventType:       mailEventRejection,
		Importance:      true,
		Confidence:      0.98,
		HydrationStatus: mailHydrationComplete,
		MatchedCompany:  "PermitFlow",
		MatchedJobTitle: "Software Engineer, New Grad",
	}

	if mailNeedsSLM(message) {
		t.Fatalf("expected clear rejection classification to skip SLM review")
	}
}

func TestMailNeedsSLMSkipsPendingHydration(t *testing.T) {
	t.Parallel()

	message := MailMessage{
		Subject:         "Application received",
		Sender:          MailAddress{Name: "Recruiting", Email: "recruiting@example.com"},
		Snippet:         "Thanks for applying.",
		EventType:       mailEventApplicationAcknowledged,
		HydrationStatus: mailHydrationPending,
	}
	if mailNeedsSLM(message) {
		t.Fatalf("expected pending hydration message to skip SLM review")
	}
}

func TestMailNeedsSLMSkipsHiringOpsDocumentRequests(t *testing.T) {
	t.Parallel()

	message := MailMessage{
		Subject:         "Reg: 2025 Tax filing",
		Sender:          MailAddress{Name: "Kavya", Email: "kavya@example.com"},
		Snippet:         "Regarding your job application for the Tax Analyst role at Acme.",
		BodyText:        "Please share the documents for the Tax Analyst role at Acme.",
		EventType:       mailEventOtherJobRelated,
		Importance:      true,
		HydrationStatus: mailHydrationComplete,
		MatchedCompany:  "Acme",
		MatchedJobTitle: "Tax Analyst",
	}
	if mailNeedsSLM(message) {
		t.Fatalf("expected hiring-ops document request to skip SLM review")
	}
}

func TestMailNeedsSLMSkipsTaxFilerSupportFollowUp(t *testing.T) {
	t.Parallel()

	message := MailMessage{
		Subject:         "unable to reach you",
		Sender:          MailAddress{Name: "Support .", Email: "support@dollartaxfiler.com"},
		BodyText:        "Our team has attempted to contact you by phone, but we were unable to reach you and were directed to voicemail. Please contact us at your earliest convenience.",
		EventType:       mailEventOtherJobRelated,
		Importance:      true,
		HydrationStatus: mailHydrationComplete,
	}
	if mailNeedsSLM(message) {
		t.Fatalf("expected tax-filer support follow-up to skip SLM review")
	}
}

func TestMailNeedsSLMSkipsAcademicEventMail(t *testing.T) {
	t.Parallel()

	message := MailMessage{
		Subject:         "FW: REMINDER -TODAY: MGEN Speaker Event - What Hiring Managers Really Look For with Kori Rahaim",
		Sender:          MailAddress{Name: "Sujendra Jayant Gharat", Email: "candidate@example.com"},
		BodyText:        "From: Macri, Erin <er.macri@northeastern.edu>\nJoin us TODAY for our next MGEN Speaker Series — Voices from the Field, Lessons for the Future!\nRegister here for Boston Campus and Network participants.\nMGEN Operations Team\nforms.office.com",
		EventType:       mailEventIgnored,
		HydrationStatus: mailHydrationComplete,
	}
	if mailNeedsSLM(message) {
		t.Fatalf("expected academic event mail to skip SLM review")
	}
}

func TestMailNeedsSLMSkipsLaddersDigest(t *testing.T) {
	t.Parallel()

	message := MailMessage{
		Subject:         "FW: Recruiters want your skills. These jobs won't wait.",
		Sender:          MailAddress{Name: "Sujendra Jayant Gharat", Email: "candidate@example.com"},
		BodyText:        "From: Ladders <jobs@my.theladders.com>\nDo these jobs match what you're looking for?\nYou're receiving this email because you signed up.\nUnsubscribe\n© 2026 Ladders, Inc.",
		EventType:       mailEventJobBoardInvite,
		HydrationStatus: mailHydrationComplete,
	}
	if mailNeedsSLM(message) {
		t.Fatalf("expected Ladders digest to skip SLM review")
	}
}

func TestHistoricalMailClassifierSupportsRecruiterOutreach(t *testing.T) {
	t.Setenv("MAIL_SLM_ENABLED", "0")

	classifier := buildMailHistoricalClassifier([]MailMessage{
		{
			Subject:    "Quick intro about your application",
			Sender:     MailAddress{Name: "Talent Partner", Email: "talent@openai.com"},
			BodyText:   "Would you be open to a quick intro next week about the role?",
			EventType:  mailEventRecruiterOutreach,
			Confidence: 0.95,
		},
		{
			Subject:    "Following up on your application",
			Sender:     MailAddress{Name: "Recruiter", Email: "recruiter@openai.com"},
			BodyText:   "Let's connect next week about your background and the team.",
			EventType:  mailEventRecruiterOutreach,
			Confidence: 0.94,
		},
		{
			Subject:    "OpenAI application follow-up",
			Sender:     MailAddress{Name: "Hiring Team", Email: "hiring@openai.com"},
			BodyText:   "Can we find time for a short intro call next week?",
			EventType:  mailEventRecruiterOutreach,
			Confidence: 0.93,
		},
		{
			Subject:    "Quick note on your candidacy",
			Sender:     MailAddress{Name: "Talent Acquisition", Email: "talent-acquisition@openai.com"},
			BodyText:   "Would love to chat about next steps for your application.",
			EventType:  mailEventRecruiterOutreach,
			Confidence: 0.96,
		},
		{
			Subject:    "Application received for Platform Engineer",
			Sender:     MailAddress{Name: "OpenAI Careers", Email: "noreply@openai.com"},
			BodyText:   "Thank you for applying. We received your application.",
			EventType:  mailEventApplicationAcknowledged,
			Confidence: 0.96,
		},
		{
			Subject:    "Thanks for applying to OpenAI",
			Sender:     MailAddress{Name: "OpenAI Careers", Email: "noreply@openai.com"},
			BodyText:   "Your application has been received.",
			EventType:  mailEventApplicationAcknowledged,
			Confidence: 0.95,
		},
		{
			Subject:    "Application submitted",
			Sender:     MailAddress{Name: "OpenAI Careers", Email: "jobs@openai.com"},
			BodyText:   "We received your submission for the role.",
			EventType:  mailEventApplicationAcknowledged,
			Confidence: 0.94,
		},
		{
			Subject:    "Your application has been received",
			Sender:     MailAddress{Name: "OpenAI", Email: "careers@openai.com"},
			BodyText:   "Thank you for your application.",
			EventType:  mailEventApplicationAcknowledged,
			Confidence: 0.94,
		},
	})
	if classifier == nil {
		t.Fatal("expected historical classifier to be built")
	}

	context := mailMatchContext{
		Companies:            []string{"OpenAI"},
		HistoricalClassifier: classifier,
	}
	message := MailMessage{
		Subject:  "About your application",
		Sender:   MailAddress{Name: "Talent Team", Email: "talent@openai.com"},
		BodyText: "Would you be open to a quick intro next week?",
	}

	classifyMailMessage(&message, context)

	if message.EventType != mailEventRecruiterOutreach {
		t.Fatalf("event_type = %q, want %q", message.EventType, mailEventRecruiterOutreach)
	}
	if message.DecisionSource != "rules" {
		t.Fatalf("decision source = %q, want %q", message.DecisionSource, "rules")
	}
	if message.Confidence < 0.82 {
		t.Fatalf("confidence = %v, want >= 0.82", message.Confidence)
	}
	if !containsFoldedReason(message.Reasons, "Historical corpus classifier matched prior recruiter-outreach patterns") {
		t.Fatalf("expected historical-classifier reason in %v", message.Reasons)
	}
}

func TestHistoricalMailClassifierDoesNotOverrideApplicationProgressToRecruiter(t *testing.T) {
	t.Setenv("MAIL_SLM_ENABLED", "0")

	classifier := buildMailHistoricalClassifier([]MailMessage{
		{
			Subject:    "Quick intro about your application",
			Sender:     MailAddress{Name: "Talent Partner", Email: "talent@openai.com"},
			BodyText:   "Would you be open to a quick intro next week about the role?",
			EventType:  mailEventRecruiterOutreach,
			Confidence: 0.95,
		},
		{
			Subject:    "Following up on your application",
			Sender:     MailAddress{Name: "Recruiter", Email: "recruiter@openai.com"},
			BodyText:   "Let's connect next week about your background and the team.",
			EventType:  mailEventRecruiterOutreach,
			Confidence: 0.94,
		},
		{
			Subject:    "OpenAI application follow-up",
			Sender:     MailAddress{Name: "Hiring Team", Email: "hiring@openai.com"},
			BodyText:   "Can we find time for a short intro call next week?",
			EventType:  mailEventRecruiterOutreach,
			Confidence: 0.93,
		},
		{
			Subject:    "Quick note on your candidacy",
			Sender:     MailAddress{Name: "Talent Acquisition", Email: "talent-acquisition@openai.com"},
			BodyText:   "Would love to chat about next steps for your application.",
			EventType:  mailEventRecruiterOutreach,
			Confidence: 0.96,
		},
		{
			Subject:    "Application received for Platform Engineer",
			Sender:     MailAddress{Name: "OpenAI Careers", Email: "noreply@openai.com"},
			BodyText:   "Thank you for applying. We received your application.",
			EventType:  mailEventApplicationAcknowledged,
			Confidence: 0.96,
		},
		{
			Subject:    "Your application has been received",
			Sender:     MailAddress{Name: "OpenAI", Email: "careers@openai.com"},
			BodyText:   "Thank you for your application.",
			EventType:  mailEventApplicationAcknowledged,
			Confidence: 0.94,
		},
		{
			Subject:    "Thanks for applying to OpenAI",
			Sender:     MailAddress{Name: "OpenAI Careers", Email: "noreply@openai.com"},
			BodyText:   "Your application has been received.",
			EventType:  mailEventApplicationAcknowledged,
			Confidence: 0.95,
		},
		{
			Subject:    "Application submitted",
			Sender:     MailAddress{Name: "OpenAI Careers", Email: "jobs@openai.com"},
			BodyText:   "We received your submission for the role.",
			EventType:  mailEventApplicationAcknowledged,
			Confidence: 0.94,
		},
	})
	if classifier == nil {
		t.Fatal("expected historical classifier to be built")
	}

	context := mailMatchContext{
		Companies:            []string{"OpenAI"},
		HistoricalClassifier: classifier,
	}
	message := MailMessage{
		Subject: "Complete Your OpenAI Job Application",
		Sender:  MailAddress{Name: "OpenAI Talent Acquisition", Email: "talentacquisition@careers.openai.com"},
		Snippet: "Hi there, thank you for your interest in exploring career opportunities at OpenAI. " +
			"We noticed that you recently started a job application and encourage you to complete it.",
	}

	classifyMailMessage(&message, context)

	if message.EventType == mailEventRecruiterReply || message.EventType == mailEventRecruiterOutreach {
		t.Fatalf("event_type = %q, want non-recruiter classification", message.EventType)
	}
	if message.DecisionSource == "historical" && containsFoldedReason(message.Reasons, "Historical corpus classifier matched prior recruiter-outreach patterns") {
		t.Fatalf("did not expect historical recruiter-outreach override, got %v", message.Reasons)
	}
	if containsFoldedReason(message.Reasons, "Historical corpus classifier matched prior recruiter-reply patterns") ||
		containsFoldedReason(message.Reasons, "Historical corpus classifier matched prior recruiter-outreach patterns") {
		t.Fatalf("did not expect historical recruiter reasoning, got %v", message.Reasons)
	}
}

func TestNormalizeReadMailMessageDowngradesTentativeInterviewAcknowledgement(t *testing.T) {
	t.Parallel()

	message := MailMessage{
		Subject: "Your application has been received",
		Sender:  MailAddress{Name: "Cribl Talent Acquisition", Email: "noreply@cribl.io"},
		BodyText: "Thank you for your interest in the Software Engineer, Stream Integrations role at Cribl - your application has been received! " +
			"What to Expect: If your background and experience seem like a good fit for the position, we will contact you soon to schedule a preliminary interview. Stay tuned for more!",
		EventType:    mailEventInterviewScheduled,
		Importance:   true,
		Confidence:   0.82,
		Reasons:      []string{"Interview scheduling language detected"},
		TriageStatus: mailTriageNew,
	}

	normalizeReadMailMessage(&message)

	if message.EventType != mailEventApplicationAcknowledged {
		t.Fatalf("event_type = %q, want %q", message.EventType, mailEventApplicationAcknowledged)
	}
	if message.Importance {
		t.Fatalf("importance = true, want false")
	}
	if message.Confidence < 0.94 {
		t.Fatalf("confidence = %v, want >= 0.94", message.Confidence)
	}
	if containsFoldedReason(message.Reasons, "Interview scheduling language detected") {
		t.Fatalf("expected stale interview scheduling reason to be removed, got %v", message.Reasons)
	}
	if !containsFoldedReason(message.Reasons, "Tentative interview next-step language detected") {
		t.Fatalf("expected corrective tentative-interview reason, got %v", message.Reasons)
	}
}

func TestNormalizeReadMailMessageDowngradesStoredRecruiterTemplateToAcknowledgement(t *testing.T) {
	t.Parallel()

	message := MailMessage{
		Subject: "Thank you for your application to Veolia North America.",
		Sender:  MailAddress{Name: "Veolia", Email: "notification@smartrecruiters.com"},
		Snippet: "Sujendra Jayant, Thank you for applying for the AI Engineering Intern (SLM) at Veolia North America. " +
			"Your application is currently being carefully reviewed by our hiring team. If your skills and experience align with this opportunity, a member of our team will contact you.",
		EventType:    mailEventRecruiterReply,
		Importance:   true,
		Confidence:   0.9,
		Reasons:      []string{"Recruiter reply or outreach signal detected"},
		TriageStatus: mailTriageNew,
	}

	normalizeReadMailMessage(&message)

	if message.EventType != mailEventApplicationAcknowledged {
		t.Fatalf("event_type = %q, want %q", message.EventType, mailEventApplicationAcknowledged)
	}
	if message.Importance {
		t.Fatalf("importance = true, want false")
	}
	if containsFoldedReason(message.Reasons, "Direct recruiter reply signal detected") ||
		containsFoldedReason(message.Reasons, "Recruiter reply or outreach signal detected") {
		t.Fatalf("expected stale recruiter-reply reason to be removed, got %v", message.Reasons)
	}
	if !containsFoldedReason(message.Reasons, "Application acknowledgement language detected") {
		t.Fatalf("expected acknowledgement reason, got %v", message.Reasons)
	}
}

func TestNormalizeReadMailMessageDowngradesStoredRecruiterTemplateToProgress(t *testing.T) {
	t.Parallel()

	message := MailMessage{
		Subject: "Complete Your Abbott Job Application",
		Sender:  MailAddress{Name: "Abbott Talent Acquisition", Email: "talentacquisition@careers.abbott.com"},
		Snippet: "Hi Sujendra Jayant, Thank you for your interest in exploring career opportunities at Abbott. " +
			"We noticed that you recently started a job application and encourage you to complete it.",
		EventType:    mailEventRecruiterReply,
		Importance:   true,
		Confidence:   0.9,
		Reasons:      []string{"Recruiter reply or outreach signal detected"},
		TriageStatus: mailTriageNew,
	}

	normalizeReadMailMessage(&message)

	if message.EventType != mailEventOtherJobRelated {
		t.Fatalf("event_type = %q, want %q", message.EventType, mailEventOtherJobRelated)
	}
	if message.Importance {
		t.Fatalf("importance = true, want false")
	}
	if containsFoldedReason(message.Reasons, "Direct recruiter reply signal detected") ||
		containsFoldedReason(message.Reasons, "Recruiter reply or outreach signal detected") {
		t.Fatalf("expected stale recruiter-reply reason to be removed, got %v", message.Reasons)
	}
	if !containsFoldedReason(message.Reasons, "Application progress reminder detected") {
		t.Fatalf("expected progress reason, got %v", message.Reasons)
	}
}

func TestNormalizeReadMailMessageDowngradesStoredRecruiterTemplateToIgnoredIndiaInvite(t *testing.T) {
	t.Parallel()

	message := MailMessage{
		Subject:      "Job | AI/ML Engineer in Bengaluru",
		Sender:       MailAddress{Name: "Naukri", Email: "alerts@naukri.com"},
		Snippet:      "Job invite from recruiter. Apply now! You've been chosen from a large pool of jobseekers to apply for this job.",
		EventType:    mailEventRecruiterReply,
		Importance:   true,
		Confidence:   0.9,
		Reasons:      []string{"Recruiter reply or outreach signal detected"},
		TriageStatus: mailTriageNew,
	}

	normalizeReadMailMessage(&message)

	if message.EventType != mailEventIgnored {
		t.Fatalf("event_type = %q, want %q", message.EventType, mailEventIgnored)
	}
	if message.Importance {
		t.Fatalf("importance = true, want false")
	}
	if message.TriageStatus != mailTriageIgnored {
		t.Fatalf("triage_status = %q, want %q", message.TriageStatus, mailTriageIgnored)
	}
	if containsFoldedReason(message.Reasons, "Direct recruiter reply signal detected") ||
		containsFoldedReason(message.Reasons, "Recruiter reply or outreach signal detected") {
		t.Fatalf("expected stale recruiter-reason to be removed, got %v", message.Reasons)
	}
	if !containsFoldedReason(message.Reasons, "India-market invited-to-apply mail ignored") {
		t.Fatalf("expected ignored India-invite reason, got %v", message.Reasons)
	}
}

func TestNormalizeReadMailMessageDowngradesStoredRecruiterReplyGitHubThread(t *testing.T) {
	t.Parallel()

	message := MailMessage{
		Subject:           "Re: [openclaw/openclaw] Agents: force loopback for sessions_spawn gateway calls (PR #29871)",
		Sender:            MailAddress{Name: "openclaw-barnacle[bot]", Email: "notifications@github.com"},
		InternetMessageID: "<openclaw/openclaw/pull/29871/c4009408809@github.com>",
		BodyText: "openclaw-barnacle[bot] left a comment (openclaw/openclaw#29871)\n\n" +
			"Reply to this email directly or view it on GitHub:\n" +
			"https://github.com/openclaw/openclaw/pull/29871#issuecomment-4009408809\n" +
			"You are receiving this because you authored the thread.",
		EventType:       mailEventRecruiterReply,
		Importance:      true,
		Confidence:      0.93,
		MatchedCompany:  "Openclaw",
		MatchedJobTitle: "Engineer",
		Reasons:         []string{"Matched tracked company", "Matched tracked job title", "Direct recruiter reply signal detected"},
		TriageStatus:    mailTriageNew,
	}

	normalizeReadMailMessage(&message)

	if message.EventType != mailEventIgnored {
		t.Fatalf("event_type = %q, want %q", message.EventType, mailEventIgnored)
	}
	if message.TriageStatus != mailTriageIgnored {
		t.Fatalf("triage_status = %q, want %q", message.TriageStatus, mailTriageIgnored)
	}
	if message.Importance {
		t.Fatalf("importance = true, want false")
	}
	if message.MatchedCompany != "" {
		t.Fatalf("matched_company = %q, want empty", message.MatchedCompany)
	}
	if message.MatchedJobTitle != "" {
		t.Fatalf("matched_job_title = %q, want empty", message.MatchedJobTitle)
	}
	if containsFoldedReason(message.Reasons, "Direct recruiter reply signal detected") {
		t.Fatalf("expected recruiter-reply reason to be removed, got %v", message.Reasons)
	}
	if containsFoldedReason(message.Reasons, "Matched tracked company") || containsFoldedReason(message.Reasons, "Matched tracked job title") {
		t.Fatalf("expected tracked-match reasons to be removed, got %v", message.Reasons)
	}
	if !containsFoldedReason(message.Reasons, "GitHub thread notification ignored") {
		t.Fatalf("expected GitHub ignore reason, got %v", message.Reasons)
	}
}

func TestNormalizeReadMailMessageDowngradesStoredRecruiterReplyLaddersDigest(t *testing.T) {
	t.Parallel()

	message := MailMessage{
		Subject:      "FW: Recruiters want your skills. These jobs won't wait.",
		Sender:       MailAddress{Name: "Sujendra Jayant Gharat", Email: "candidate@example.com"},
		BodyText:     "From: Ladders <jobs@my.theladders.com>\nDo these jobs match what you're looking for?\nYou're receiving this email because you signed up.\nUnsubscribe\n© 2026 Ladders, Inc.",
		EventType:    mailEventRecruiterReply,
		Importance:   true,
		Confidence:   0.95,
		MatchedCompany:  "Sujendra Jayant Gharat",
		MatchedJobTitle: "Software Engineer II",
		Reasons:      []string{"Matched tracked company", "Matched tracked job title", "Direct recruiter reply signal detected"},
		TriageStatus: mailTriageNew,
	}

	normalizeReadMailMessage(&message)

	if message.EventType != mailEventJobBoardInvite {
		t.Fatalf("event_type = %q, want %q", message.EventType, mailEventJobBoardInvite)
	}
	if message.Importance {
		t.Fatalf("importance = true, want false")
	}
	if message.MatchedCompany != "" {
		t.Fatalf("matched_company = %q, want empty", message.MatchedCompany)
	}
	if message.MatchedJobTitle != "" {
		t.Fatalf("matched_job_title = %q, want empty", message.MatchedJobTitle)
	}
	if containsFoldedReason(message.Reasons, "Direct recruiter reply signal detected") ||
		containsFoldedReason(message.Reasons, "Matched tracked company") ||
		containsFoldedReason(message.Reasons, "Matched tracked job title") {
		t.Fatalf("expected stale recruiter/match reasons to be removed, got %v", message.Reasons)
	}
	if !containsFoldedReason(message.Reasons, "Job-board invite template detected") {
		t.Fatalf("expected job-board reason, got %v", message.Reasons)
	}
}

func TestNormalizeReadMailMessageDowngradesStoredJobMailToIgnoredAcademicEvent(t *testing.T) {
	t.Parallel()

	message := MailMessage{
		Subject: "FW: REMINDER -TODAY: MGEN Speaker Event - What Hiring Managers Really Look For with Kori Rahaim",
		Sender:  MailAddress{Name: "Sujendra Jayant Gharat", Email: "candidate@example.com"},
		BodyText: "From: Macri, Erin <er.macri@northeastern.edu>\n" +
			"Dear MGEN Students,\nJoin us TODAY for our next MGEN Speaker Series — Voices from the Field, Lessons for the Future!\n" +
			"Location: Behrakis 315 (Boston Campus) and Online (for Network participants)\n" +
			"Register here:\nBoston Students (In-Person) – Fill out form\nNetwork Students (Virtual) – Fill out form\n" +
			"Best,\nMGEN Operations Team\nInformation and Software Engineering\nforms.office.com",
		EventType:       mailEventRecruiterReply,
		Importance:      true,
		Confidence:      0.95,
		MatchedCompany:  "Microsoft",
		MatchedJobTitle: "Software Engineering",
		Reasons:         []string{"Matched tracked company", "Matched tracked job title", "Direct recruiter reply signal detected"},
		TriageStatus:    mailTriageNew,
	}

	normalizeReadMailMessage(&message)

	if message.EventType != mailEventIgnored {
		t.Fatalf("event_type = %q, want %q", message.EventType, mailEventIgnored)
	}
	if message.TriageStatus != mailTriageIgnored {
		t.Fatalf("triage_status = %q, want %q", message.TriageStatus, mailTriageIgnored)
	}
	if message.Importance {
		t.Fatalf("importance = true, want false")
	}
	if message.MatchedCompany != "" {
		t.Fatalf("matched_company = %q, want empty", message.MatchedCompany)
	}
	if message.MatchedJobTitle != "" {
		t.Fatalf("matched_job_title = %q, want empty", message.MatchedJobTitle)
	}
	if containsFoldedReason(message.Reasons, "Direct recruiter reply signal detected") ||
		containsFoldedReason(message.Reasons, "Matched tracked company") ||
		containsFoldedReason(message.Reasons, "Matched tracked job title") {
		t.Fatalf("expected stale recruiter/match reasons to be removed, got %v", message.Reasons)
	}
	if !containsFoldedReason(message.Reasons, "Academic or campus event ignored") {
		t.Fatalf("expected academic-event reason, got %v", message.Reasons)
	}
}

func TestNormalizeReadMailMessageDowngradesStoredRecruiterReplyHiringOpsRequest(t *testing.T) {
	t.Parallel()

	message := MailMessage{
		Subject:         "Reg: 2025 Tax filing",
		Sender:          MailAddress{Name: "Kavya", Email: "kavya@example.com"},
		Snippet:         "Regarding your job application for the Tax Analyst role at Acme.",
		BodyText:        "Hi Sujendra, as we discussed over the call regarding the documents, can you please share the documents for the Tax Analyst role at Acme?",
		EventType:       mailEventRecruiterReply,
		Importance:      true,
		Confidence:      0.9,
		MatchedCompany:  "Acme",
		MatchedJobTitle: "Tax Analyst",
		Reasons:         []string{"Matched tracked company", "Matched tracked job title", "Direct recruiter reply signal detected"},
		TriageStatus:    mailTriageNew,
	}

	normalizeReadMailMessage(&message)

	if message.EventType != mailEventOtherJobRelated {
		t.Fatalf("event_type = %q, want %q", message.EventType, mailEventOtherJobRelated)
	}
	if !message.Importance {
		t.Fatalf("importance = false, want true")
	}
	if containsFoldedReason(message.Reasons, "Direct recruiter reply signal detected") {
		t.Fatalf("expected recruiter-reply reason to be removed, got %v", message.Reasons)
	}
	if !containsFoldedReason(message.Reasons, "Hiring operations/document request detected") {
		t.Fatalf("expected hiring-ops reason, got %v", message.Reasons)
	}
}

func TestNormalizeReadMailMessageDowngradesStoredRecruiterReplyTaxFilerSupportFollowUp(t *testing.T) {
	t.Parallel()

	message := MailMessage{
		Subject:      "unable to reach you",
		Sender:       MailAddress{Name: "Support .", Email: "support@dollartaxfiler.com"},
		BodyText:     "Our team has attempted to contact you by phone, but we were unable to reach you and were directed to voicemail. Please contact us at your earliest convenience.",
		EventType:    mailEventRecruiterReply,
		Importance:   true,
		Confidence:   0.95,
		Reasons:      []string{"Direct recruiter reply signal detected"},
		TriageStatus: mailTriageNew,
	}

	normalizeReadMailMessage(&message)

	if message.EventType != mailEventOtherJobRelated {
		t.Fatalf("event_type = %q, want %q", message.EventType, mailEventOtherJobRelated)
	}
	if !message.Importance {
		t.Fatalf("importance = false, want true")
	}
	if containsFoldedReason(message.Reasons, "Direct recruiter reply signal detected") {
		t.Fatalf("expected stale recruiter-reply reason to be removed, got %v", message.Reasons)
	}
	if !containsFoldedReason(message.Reasons, "Hiring operations/document request detected") {
		t.Fatalf("expected hiring-ops reason, got %v", message.Reasons)
	}
}

func TestNormalizeReadMailMessageDowngradesStoredInterviewTemplateToIgnoredIndiaInvite(t *testing.T) {
	t.Parallel()

	message := MailMessage{
		Subject:         "Job | F2F Interview - 11th March - Mumbai - AI/ML engineer",
		Sender:          MailAddress{Name: "Natalie Consultants", Email: "sakshi@naukri.com"},
		Snippet:         "Job invite from recruiter. Apply now! You've been chosen from a large pool of jobseekers to apply for this job.",
		BodyText:        "Job invite from recruiter. Apply now! You've been chosen from a large pool of jobseekers to apply for this job. F2F Interview - 11th March - Mumbai - AI/ML engineer.",
		EventType:       mailEventInterviewScheduled,
		Importance:      true,
		Confidence:      1,
		Reasons:         []string{"Interview scheduling language detected", "Matched tracked company", "Matched tracked job title"},
		MatchedCompany:  "Apple",
		MatchedJobTitle: "Profile",
		TriageStatus:    mailTriageNew,
	}

	normalizeReadMailMessage(&message)

	if message.EventType != mailEventIgnored {
		t.Fatalf("event_type = %q, want %q", message.EventType, mailEventIgnored)
	}
	if message.Importance {
		t.Fatalf("importance = true, want false")
	}
	if message.TriageStatus != mailTriageIgnored {
		t.Fatalf("triage_status = %q, want %q", message.TriageStatus, mailTriageIgnored)
	}
	if message.MatchedCompany != "" {
		t.Fatalf("matched company = %q, want empty string", message.MatchedCompany)
	}
	if message.MatchedJobTitle != "" {
		t.Fatalf("matched job title = %q, want empty string", message.MatchedJobTitle)
	}
	if containsFoldedReason(message.Reasons, "Interview scheduling language detected") ||
		containsFoldedReason(message.Reasons, "Matched tracked company") ||
		containsFoldedReason(message.Reasons, "Matched tracked job title") {
		t.Fatalf("expected stale interview/match reasons to be removed, got %v", message.Reasons)
	}
	if !containsFoldedReason(message.Reasons, "India-market invited-to-apply mail ignored") {
		t.Fatalf("expected ignored India-invite reason, got %v", message.Reasons)
	}
}

func TestNormalizeReadMailMessageDowngradesStoredRecruiterTemplateToIndiaMarket(t *testing.T) {
	t.Parallel()

	message := MailMessage{
		Subject:      "Urgent Vacancy - Verify Profile Details",
		Sender:       MailAddress{Name: "Palm HR Consultants", Email: "alerts@jobs.shine.com"},
		Snippet:      "Dear Sujendra, I reviewed your profile for an urgent opportunity and need a quick confirmation of a few details before I share your CV with the hiring manager.",
		EventType:    mailEventRecruiterReply,
		Importance:   true,
		Confidence:   0.9,
		Reasons:      []string{"Recruiter reply or outreach signal detected"},
		TriageStatus: mailTriageNew,
	}

	normalizeReadMailMessage(&message)

	if message.EventType != mailEventIndiaJobMarket {
		t.Fatalf("event_type = %q, want %q", message.EventType, mailEventIndiaJobMarket)
	}
	if message.Importance {
		t.Fatalf("importance = true, want false")
	}
	if !containsFoldedReason(message.Reasons, "India-market job platform mail detected") {
		t.Fatalf("expected India-market reason, got %v", message.Reasons)
	}
}

func TestNormalizeReadMailMessageUpgradesStoredJobMailToRejection(t *testing.T) {
	t.Parallel()

	message := MailMessage{
		Subject:         "Update on your candidacy from Dropbox",
		Sender:          MailAddress{Name: "no-reply@dropbox.com", Email: "no-reply@dropbox.com"},
		BodyText:        "We've reviewed your application for the Software Engineer (c) role, but after careful consideration, we've decided not to move forward at this time.",
		EventType:       mailEventOtherJobRelated,
		Importance:      false,
		Confidence:      0.84,
		Reasons:         []string{"Application progress reminder detected"},
		MatchedCompany:  "Dropbox",
		MatchedJobTitle: "Software Engineer (c)",
		TriageStatus:    mailTriageNew,
	}

	normalizeReadMailMessage(&message)

	if message.EventType != mailEventRejection {
		t.Fatalf("event_type = %q, want %q", message.EventType, mailEventRejection)
	}
	if !message.Importance {
		t.Fatalf("importance = false, want true")
	}
	if message.Confidence < 0.98 {
		t.Fatalf("confidence = %v, want >= 0.98", message.Confidence)
	}
	if !containsFoldedReason(message.Reasons, "Rejection language detected") {
		t.Fatalf("expected rejection reason, got %v", message.Reasons)
	}
	if containsFoldedReason(message.Reasons, "Application progress reminder detected") {
		t.Fatalf("expected stale progress reason to be removed, got %v", message.Reasons)
	}
}

func TestNormalizeReadMailMessageDowngradesStoredNewsletterToIgnored(t *testing.T) {
	t.Parallel()

	message := MailMessage{
		Subject:         "The Orange Line ATE with this ...",
		Sender:          MailAddress{Name: "The B-Side", Email: "thebside@mail.boston.com"},
		Snippet:         "Plus: Parking ticket scam alert!",
		BodyText:        "Read Online. What's on tap today: parking ticket scam alert, things to do, start a membership.",
		EventType:       mailEventInterviewUpdated,
		Importance:      true,
		Confidence:      0.9,
		Reasons:         []string{"Interview reschedule/update language detected", "Matched tracked job title"},
		MatchedJobTitle: "Copilot",
		TriageStatus:    mailTriageNew,
	}

	normalizeReadMailMessage(&message)

	if message.EventType != mailEventIgnored {
		t.Fatalf("event_type = %q, want %q", message.EventType, mailEventIgnored)
	}
	if message.Importance {
		t.Fatalf("importance = true, want false")
	}
	if message.TriageStatus != mailTriageIgnored {
		t.Fatalf("triage_status = %q, want %q", message.TriageStatus, mailTriageIgnored)
	}
	if message.MatchedJobTitle != "" {
		t.Fatalf("matched job title = %q, want empty string", message.MatchedJobTitle)
	}
	if containsFoldedReason(message.Reasons, "Interview reschedule/update language detected") ||
		containsFoldedReason(message.Reasons, "Matched tracked job title") {
		t.Fatalf("expected stale interview/match reasons to be removed, got %v", message.Reasons)
	}
	if !containsFoldedReason(message.Reasons, "Local/newsletter digest ignored") {
		t.Fatalf("expected newsletter ignore reason, got %v", message.Reasons)
	}
}

func TestNormalizeReadMailMessageWithContextClearsWeakStoredCompanyMatch(t *testing.T) {
	t.Parallel()

	context := mailMatchContext{
		Companies: []string{"Box", "OpenAI"},
	}
	message := MailMessage{
		Subject:        "Thank you for your application",
		Sender:         MailAddress{Name: "Talent Acquisition", Email: "noreply@example.com"},
		BodyText:       "Thank you for applying. This email box is not monitored.",
		MatchedCompany: "Box",
		EventType:      mailEventApplicationAcknowledged,
	}

	normalizeReadMailMessageWithContext(&message, context)

	if message.MatchedCompany != "" {
		t.Fatalf("matched company = %q, want empty string", message.MatchedCompany)
	}
	if containsFoldedReason(message.Reasons, "Matched tracked company") {
		t.Fatalf("expected stale matched-company reason to be removed, got %v", message.Reasons)
	}
}

func TestNormalizeReadMailMessageWithContextClearsIndiaInviteTrackedMatches(t *testing.T) {
	t.Parallel()

	context := mailMatchContext{
		Companies: []string{"Apple", "OpenAI"},
		Jobs: []mailKnownJob{
			{Company: "Apple", Title: "Profile"},
			{Company: "Apple", Title: "AI Engineer"},
		},
	}
	message := MailMessage{
		Subject:         "Job | F2F Interview - 11th March - Mumbai - AI/ML engineer",
		Sender:          MailAddress{Name: "Natalie Consultants", Email: "sakshi@naukri.com"},
		BodyText:        "Job invite from recruiter. Apply now! You've been chosen from a large pool of jobseekers to apply for this job. F2F Interview - 11th March - Mumbai - AI/ML engineer.",
		EventType:       mailEventInterviewScheduled,
		MatchedCompany:  "Apple",
		MatchedJobTitle: "Profile",
		Reasons:         []string{"Matched tracked company", "Matched tracked job title"},
	}

	normalizeReadMailMessageWithContext(&message, context)

	if message.MatchedCompany != "" {
		t.Fatalf("matched company = %q, want empty string", message.MatchedCompany)
	}
	if message.MatchedJobTitle != "" {
		t.Fatalf("matched job title = %q, want empty string", message.MatchedJobTitle)
	}
	if containsFoldedReason(message.Reasons, "Matched tracked company") || containsFoldedReason(message.Reasons, "Matched tracked job title") {
		t.Fatalf("expected stale tracked-match reasons to be removed, got %v", message.Reasons)
	}
}

func writeMailMatchContextTestConfig(t *testing.T, dir string, companyName string) string {
	t.Helper()
	path := filepath.Join(dir, "companies.yaml")
	body := "companies:\n" +
		"  - name: " + companyName + "\n" +
		"    source: greenhouse\n" +
		"    careers_url: https://example.com/careers\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func seedMailMatchContextFixture(t *testing.T, tempDir string) (MailServiceConfig, int64) {
	t.Helper()
	statePath := filepath.Join(tempDir, "openings_state.json")
	t.Setenv("MAIL_DB_PATH", filepath.Join(tempDir, "mail.db"))
	t.Setenv("JOBS_DB_PATH", filepath.Join(tempDir, "jobs.db"))
	resetMailMatchContextCache()
	t.Cleanup(resetMailMatchContextCache)

	jobsDB, err := openJobsDB(jobsDBPath(statePath))
	if err != nil {
		t.Fatalf("openJobsDB error: %v", err)
	}
	if err := jobsDB.Close(); err != nil {
		t.Fatalf("close jobs db: %v", err)
	}

	account, err := upsertMailAccount(statePath, mailStoredAccount{
		MailAccount: MailAccount{
			Provider:    "gmail",
			Email:       "candidate@example.com",
			DisplayName: "Candidate",
			Status:      "connected",
			Scopes:      []string{"gmail.readonly"},
			ConnectedAt: utcNow(),
		},
		TokenJSON: `{"access_token":"token"}`,
	})
	if err != nil {
		t.Fatalf("upsertMailAccount error: %v", err)
	}

	if _, _, err := upsertMailMessages(statePath, mailStoredAccount{MailAccount: account}, []MailMessage{
		{
			Provider:          "gmail",
			ProviderMessageID: "cache-fixture-message",
			ProviderThreadID:  "cache-fixture-thread",
			Subject:           "Interview confirmation",
			Sender:            MailAddress{Name: "Recruiter", Email: "recruiter@openai.com"},
			ReceivedAt:        utcNow(),
			Snippet:           "Your interview is scheduled.",
			BodyText:          "Your interview is scheduled for tomorrow.",
			MatchedCompany:    "OpenAI",
			MatchedJobTitle:   "Software Engineer",
			EventType:         mailEventInterviewScheduled,
			Importance:        true,
			Confidence:        0.95,
			DecisionSource:    "rules",
			Reasons:           []string{"Calendar invite metadata detected"},
			TriageStatus:      mailTriageNew,
			HasInvite:         true,
		},
	}); err != nil {
		t.Fatalf("upsertMailMessages error: %v", err)
	}

	db, err := openMailDB(statePath)
	if err != nil {
		t.Fatalf("openMailDB error: %v", err)
	}
	var messageID int64
	if err := db.QueryRow(`SELECT id FROM mail_messages LIMIT 1`).Scan(&messageID); err != nil {
		t.Fatalf("load message id: %v", err)
	}
	return MailServiceConfig{StatePath: statePath}, messageID
}

func TestMailMatchContextCacheHitsAcrossReadPaths(t *testing.T) {
	tempDir := t.TempDir()
	cfg, messageID := seedMailMatchContextFixture(t, tempDir)

	list, err := loadMailMessagesWithConfig(cfg, MailMessageFilters{Limit: 10})
	if err != nil {
		t.Fatalf("loadMailMessagesWithConfig error: %v", err)
	}
	if len(list.Messages) != 1 {
		t.Fatalf("messages len = %d, want 1", len(list.Messages))
	}
	if builds := mailMatchContextBuildCount(); builds != 1 {
		t.Fatalf("build count after message load = %d, want 1", builds)
	}

	analytics, err := loadMailAnalyticsWithConfig(cfg, MailMessageFilters{})
	if err != nil {
		t.Fatalf("loadMailAnalyticsWithConfig error: %v", err)
	}
	if analytics.Summary.ConnectedAccountsCount != 1 {
		t.Fatalf("connected accounts = %d, want 1", analytics.Summary.ConnectedAccountsCount)
	}
	if builds := mailMatchContextBuildCount(); builds != 1 {
		t.Fatalf("build count after analytics load = %d, want 1", builds)
	}

	detail, err := loadMailMessageDetailWithConfig(cfg, messageID)
	if err != nil {
		t.Fatalf("loadMailMessageDetailWithConfig error: %v", err)
	}
	if detail.Subject != "Interview confirmation" {
		t.Fatalf("subject = %q, want %q", detail.Subject, "Interview confirmation")
	}
	if builds := mailMatchContextBuildCount(); builds != 1 {
		t.Fatalf("build count after detail load = %d, want 1", builds)
	}
}

func TestMailMatchContextCacheInvalidatesOnJobsDBChange(t *testing.T) {
	tempDir := t.TempDir()
	cfg, _ := seedMailMatchContextFixture(t, tempDir)

	if _, err := loadMailMessagesWithConfig(cfg, MailMessageFilters{Limit: 10}); err != nil {
		t.Fatalf("initial loadMailMessagesWithConfig error: %v", err)
	}
	if builds := mailMatchContextBuildCount(); builds != 1 {
		t.Fatalf("initial build count = %d, want 1", builds)
	}

	now := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(jobsDBPath(cfg.StatePath), now, now); err != nil {
		t.Fatalf("touch jobs db: %v", err)
	}

	if _, err := loadMailAnalyticsWithConfig(cfg, MailMessageFilters{}); err != nil {
		t.Fatalf("loadMailAnalyticsWithConfig after jobs db change error: %v", err)
	}
	if builds := mailMatchContextBuildCount(); builds != 2 {
		t.Fatalf("build count after jobs db change = %d, want 2", builds)
	}
}

func TestMailMatchContextCacheInvalidatesOnConfigChange(t *testing.T) {
	tempDir := t.TempDir()
	cfg, _ := seedMailMatchContextFixture(t, tempDir)
	cfg.ConfigPath = writeMailMatchContextTestConfig(t, tempDir, "OpenAI")

	if _, err := loadMailMessagesWithConfig(cfg, MailMessageFilters{Limit: 10}); err != nil {
		t.Fatalf("initial loadMailMessagesWithConfig error: %v", err)
	}
	if builds := mailMatchContextBuildCount(); builds != 1 {
		t.Fatalf("initial build count = %d, want 1", builds)
	}

	cfg.ConfigPath = writeMailMatchContextTestConfig(t, tempDir, "OpenAI Labs")
	if _, err := loadMailAnalyticsWithConfig(cfg, MailMessageFilters{}); err != nil {
		t.Fatalf("loadMailAnalyticsWithConfig after config change error: %v", err)
	}
	if builds := mailMatchContextBuildCount(); builds != 2 {
		t.Fatalf("build count after config change = %d, want 2", builds)
	}
}

func TestMailMatchContextCacheDoesNotStoreFailedBuilds(t *testing.T) {
	tempDir := t.TempDir()
	cfg, _ := seedMailMatchContextFixture(t, tempDir)
	cfg.ConfigPath = filepath.Join(tempDir, "missing-companies.yaml")

	if _, err := loadMailMessagesWithConfig(cfg, MailMessageFilters{Limit: 10}); err != nil {
		t.Fatalf("loadMailMessagesWithConfig error: %v", err)
	}
	if builds := mailMatchContextBuildCount(); builds != 1 {
		t.Fatalf("build count after first failed config load = %d, want 1", builds)
	}

	if _, err := loadMailAnalyticsWithConfig(cfg, MailMessageFilters{}); err != nil {
		t.Fatalf("loadMailAnalyticsWithConfig error: %v", err)
	}
	if builds := mailMatchContextBuildCount(); builds != 2 {
		t.Fatalf("build count after second failed config load = %d, want 2", builds)
	}
}

func TestMailDBRoundTripAndTriageUpdate(t *testing.T) {
	tempDir := t.TempDir()
	statePath := filepath.Join(tempDir, "openings_state.json")
	t.Setenv("MAIL_DB_PATH", filepath.Join(tempDir, "mail.db"))

	account, err := upsertMailAccount(statePath, mailStoredAccount{
		MailAccount: MailAccount{
			Provider:    "gmail",
			Email:       "candidate@example.com",
			DisplayName: "Candidate",
			Status:      "connected",
			Scopes:      []string{"gmail.readonly"},
			ConnectedAt: utcNow(),
		},
		TokenJSON: `{"access_token":"token"}`,
	})
	if err != nil {
		t.Fatalf("upsertMailAccount error: %v", err)
	}

	stored, important, err := upsertMailMessages(statePath, mailStoredAccount{MailAccount: account}, []MailMessage{
		{
			Provider:          "gmail",
			ProviderMessageID: "abc123",
			ProviderThreadID:  "thread-1",
			Subject:           "Interview confirmation",
			Sender:            MailAddress{Name: "Recruiter", Email: "recruiter@openai.com"},
			ReceivedAt:        utcNow(),
			Snippet:           "Your interview is scheduled.",
			BodyText:          "Your interview is scheduled for tomorrow.",
			MatchedCompany:    "OpenAI",
			MatchedJobTitle:   "Software Engineer",
			EventType:         mailEventInterviewScheduled,
			Importance:        true,
			Confidence:        0.95,
			DecisionSource:    "rules",
			Reasons:           []string{"Calendar invite metadata detected"},
			TriageStatus:      mailTriageNew,
			HasInvite:         true,
		},
	})
	if err != nil {
		t.Fatalf("upsertMailMessages error: %v", err)
	}
	if stored != 1 || important != 1 {
		t.Fatalf("stored=%d important=%d, want 1/1", stored, important)
	}

	list, err := loadMailMessages(statePath, MailMessageFilters{UnreadOnly: false, Limit: 50})
	if err != nil {
		t.Fatalf("loadMailMessages error: %v", err)
	}
	if len(list.Messages) != 1 {
		t.Fatalf("messages len = %d, want 1", len(list.Messages))
	}
	messageID := list.Messages[0].ID
	if list.Messages[0].MatchedCompany != "OpenAI" {
		t.Fatalf("matched company = %q, want %q", list.Messages[0].MatchedCompany, "OpenAI")
	}

	detail, err := loadMailMessageDetail(statePath, messageID)
	if err != nil {
		t.Fatalf("loadMailMessageDetail error: %v", err)
	}
	if !strings.Contains(detail.BodyText, "scheduled for tomorrow") {
		t.Fatalf("detail body missing expected text: %q", detail.BodyText)
	}

	update, err := updateMailMessageTriage(statePath, messageID, mailTriageFollowUp)
	if err != nil {
		t.Fatalf("updateMailMessageTriage error: %v", err)
	}
	if update.TriageStatus != mailTriageFollowUp {
		t.Fatalf("triage status = %q, want %q", update.TriageStatus, mailTriageFollowUp)
	}

	overview, err := loadMailOverview(statePath)
	if err != nil {
		t.Fatalf("loadMailOverview error: %v", err)
	}
	if overview.Summary.UnreadImportantCount != 0 {
		t.Fatalf("unread important = %d, want 0", overview.Summary.UnreadImportantCount)
	}
	if overview.Summary.LatestInterview == nil || overview.Summary.LatestInterview.Subject != "Interview confirmation" {
		t.Fatalf("latest interview highlight missing or wrong: %#v", overview.Summary.LatestInterview)
	}

	if _, err := os.Stat(filepath.Join(tempDir, "mail.db")); err != nil {
		t.Fatalf("expected mail db file to exist: %v", err)
	}
}

func TestLoadMailAnalyticsLifecycleMatching(t *testing.T) {
	tempDir := t.TempDir()
	statePath := filepath.Join(tempDir, "openings_state.json")
	t.Setenv("MAIL_DB_PATH", filepath.Join(tempDir, "mail.db"))

	account, err := upsertMailAccount(statePath, mailStoredAccount{
		MailAccount: MailAccount{
			Provider:    "gmail",
			Email:       "candidate@example.com",
			DisplayName: "Candidate",
			Status:      "connected",
			Scopes:      []string{"gmail.readonly"},
			ConnectedAt: utcNow(),
		},
		TokenJSON: `{"access_token":"token"}`,
	})
	if err != nil {
		t.Fatalf("upsertMailAccount error: %v", err)
	}

	now := time.Now().In(time.Local)
	at := func(daysAgo int, hour int, minute int) string {
		return time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, time.Local).
			AddDate(0, 0, -daysAgo).
			Format(time.RFC3339)
	}
	meetingStart := time.Date(now.Year(), now.Month(), now.Day(), 10, 0, 0, 0, time.Local).AddDate(0, 0, 1).Format(time.RFC3339)
	meetingEnd := time.Date(now.Year(), now.Month(), now.Day(), 10, 30, 0, 0, time.Local).AddDate(0, 0, 1).Format(time.RFC3339)

	messages := []MailMessage{
		{
			Provider:          "gmail",
			ProviderMessageID: "app-old",
			Subject:           "Application received for Software Engineer",
			Sender:            MailAddress{Name: "OpenAI Careers", Email: "noreply@openai.com"},
			ReceivedAt:        at(8, 9, 0),
			MatchedCompany:    "OpenAI",
			MatchedJobTitle:   "Software Engineer",
			EventType:         mailEventApplicationAcknowledged,
			Confidence:        0.95,
			DecisionSource:    "rules",
			TriageStatus:      mailTriageReviewed,
		},
		{
			Provider:          "gmail",
			ProviderMessageID: "app-yesterday",
			Subject:           "Application submitted for Machine Learning Engineer",
			Sender:            MailAddress{Name: "OpenAI Careers", Email: "noreply@openai.com"},
			ReceivedAt:        at(1, 10, 15),
			MatchedCompany:    "OpenAI",
			MatchedJobTitle:   "Machine Learning Engineer",
			EventType:         mailEventApplicationAcknowledged,
			Confidence:        0.94,
			DecisionSource:    "rules",
			TriageStatus:      mailTriageReviewed,
		},
		{
			Provider:          "gmail",
			ProviderMessageID: "app-low-confidence",
			Subject:           "Thank you for applying at Acme",
			Sender:            MailAddress{Name: "Acme Careers", Email: "noreply@acme.com"},
			ReceivedAt:        at(0, 9, 30),
			MatchedCompany:    "Acme",
			MatchedJobTitle:   "",
			EventType:         mailEventApplicationAcknowledged,
			Confidence:        0.77,
			DecisionSource:    "rules",
			TriageStatus:      mailTriageReviewed,
		},
		{
			Provider:          "gmail",
			ProviderMessageID: "rej-match",
			Subject:           "Update on your Software Engineer application",
			Sender:            MailAddress{Name: "OpenAI Careers", Email: "noreply@openai.com"},
			ReceivedAt:        at(0, 11, 0),
			MatchedCompany:    "OpenAI",
			MatchedJobTitle:   "Software Engineer",
			EventType:         mailEventRejection,
			Importance:        true,
			Confidence:        0.98,
			DecisionSource:    "rules",
			TriageStatus:      mailTriageReviewed,
		},
		{
			Provider:          "gmail",
			ProviderMessageID: "rej-company-only",
			Subject:           "Update on your Acme application",
			Sender:            MailAddress{Name: "Acme Careers", Email: "noreply@acme.com"},
			ReceivedAt:        at(0, 12, 0),
			MatchedCompany:    "Acme",
			MatchedJobTitle:   "Platform Engineer",
			EventType:         mailEventRejection,
			Importance:        true,
			Confidence:        0.91,
			DecisionSource:    "rules",
			TriageStatus:      mailTriageReviewed,
		},
		{
			Provider:          "gmail",
			ProviderMessageID: "reply-follow-up",
			Subject:           "Re: Machine Learning Engineer application",
			Sender:            MailAddress{Name: "Recruiter", Email: "recruiter@openai.com"},
			ReceivedAt:        at(0, 13, 0),
			MatchedCompany:    "OpenAI",
			MatchedJobTitle:   "Machine Learning Engineer",
			EventType:         mailEventRecruiterReply,
			Importance:        true,
			Confidence:        0.9,
			DecisionSource:    "rules",
			TriageStatus:      mailTriageFollowUp,
			Reasons:           []string{"Recruiter reply or outreach signal detected"},
		},
		{
			Provider:          "gmail",
			ProviderMessageID: "meeting",
			Subject:           "Interview scheduled with OpenAI",
			Sender:            MailAddress{Name: "Recruiting", Email: "recruiting@openai.com"},
			ReceivedAt:        at(0, 14, 0),
			MatchedCompany:    "OpenAI",
			MatchedJobTitle:   "Machine Learning Engineer",
			EventType:         mailEventInterviewScheduled,
			Importance:        true,
			Confidence:        0.96,
			DecisionSource:    "rules",
			TriageStatus:      mailTriageImportant,
			HasInvite:         true,
			MeetingStart:      meetingStart,
			MeetingEnd:        meetingEnd,
			MeetingOrganizer:  "recruiting@openai.com",
			MeetingLocation:   "Zoom",
		},
	}

	stored, important, err := upsertMailMessages(statePath, mailStoredAccount{MailAccount: account}, messages)
	if err != nil {
		t.Fatalf("upsertMailMessages error: %v", err)
	}
	if stored != len(messages) {
		t.Fatalf("stored=%d, want %d", stored, len(messages))
	}
	if important != 4 {
		t.Fatalf("important=%d, want 4", important)
	}

	analytics, err := loadMailAnalytics(statePath, MailMessageFilters{Limit: 1})
	if err != nil {
		t.Fatalf("loadMailAnalytics error: %v", err)
	}

	if analytics.Summary.AllTime.Applications != 3 || analytics.Summary.AllTime.Rejections != 2 {
		t.Fatalf("all-time counts = %#v, want applications=3 rejections=2", analytics.Summary.AllTime)
	}
	if analytics.Summary.Today.Applications != 1 || analytics.Summary.Today.Rejections != 2 {
		t.Fatalf("today counts = %#v, want applications=1 rejections=2", analytics.Summary.Today)
	}
	if analytics.Summary.Yesterday.Applications != 1 || analytics.Summary.Yesterday.Rejections != 0 {
		t.Fatalf("yesterday counts = %#v, want applications=1 rejections=0", analytics.Summary.Yesterday)
	}
	if analytics.Summary.Last7Days.Applications != 2 || analytics.Summary.Last7Days.Rejections != 2 {
		t.Fatalf("last 7 days counts = %#v, want applications=2 rejections=2", analytics.Summary.Last7Days)
	}
	if analytics.Summary.OpenApplicationsCount != 2 {
		t.Fatalf("open applications = %d, want 2", analytics.Summary.OpenApplicationsCount)
	}
	if analytics.Summary.ResolvedRejectionsCount != 1 {
		t.Fatalf("resolved rejections = %d, want 1", analytics.Summary.ResolvedRejectionsCount)
	}
	if analytics.Summary.UnresolvedRejectionsCount != 1 {
		t.Fatalf("unresolved rejections = %d, want 1", analytics.Summary.UnresolvedRejectionsCount)
	}
	if analytics.Summary.OpenActionsCount != 2 {
		t.Fatalf("open actions = %d, want 2", analytics.Summary.OpenActionsCount)
	}
	if analytics.Summary.UpcomingMeetingsCount != 1 {
		t.Fatalf("upcoming meetings = %d, want 1", analytics.Summary.UpcomingMeetingsCount)
	}
	if len(analytics.DailyBuckets) != 7 {
		t.Fatalf("daily buckets len = %d, want 7", len(analytics.DailyBuckets))
	}

	todayKey := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local).Format("2006-01-02")
	foundToday := false
	for _, bucket := range analytics.DailyBuckets {
		if bucket.DayKey != todayKey {
			continue
		}
		foundToday = true
		if bucket.Applications != 1 || bucket.Rejections != 2 {
			t.Fatalf("today bucket = %#v, want applications=1 rejections=2", bucket)
		}
	}
	if !foundToday {
		t.Fatalf("today bucket %q not found in %#v", todayKey, analytics.DailyBuckets)
	}

	if len(analytics.Details.ResolvedRejections) != 1 || analytics.Details.ResolvedRejections[0].MatchedApplication != "Software Engineer" {
		t.Fatalf("resolved rejections = %#v, want one match for Software Engineer", analytics.Details.ResolvedRejections)
	}
	if len(analytics.Details.UnresolvedRejections) != 1 || !analytics.Details.UnresolvedRejections[0].CompanyOnlyMatch {
		t.Fatalf("unresolved rejections = %#v, want one company-only unresolved rejection", analytics.Details.UnresolvedRejections)
	}

	lowConfidenceOpenApplication := false
	for _, application := range analytics.Details.OpenApplications {
		if application.Company == "Acme" && application.LowConfidence {
			lowConfidenceOpenApplication = true
		}
		if application.JobTitle == "Software Engineer" {
			t.Fatalf("matched rejected application should not remain open: %#v", application)
		}
	}
	if !lowConfidenceOpenApplication {
		t.Fatalf("expected low-confidence open Acme application in %#v", analytics.Details.OpenApplications)
	}
}

func TestGmailOAuthCredentialsFromDownloadedJSON(t *testing.T) {
	tempDir := t.TempDir()
	jsonPath := filepath.Join(tempDir, "client_secret_test.apps.googleusercontent.com.json")
	body := `{
  "installed": {
    "client_id": "test-client.apps.googleusercontent.com",
    "client_secret": "test-secret"
  }
}`
	if err := os.WriteFile(jsonPath, []byte(body), 0o600); err != nil {
		t.Fatalf("write client json: %v", err)
	}
	t.Setenv("GOOGLE_OAUTH_CLIENT_ID", "")
	t.Setenv("GOOGLE_OAUTH_CLIENT_SECRET", "")
	t.Setenv("GOOGLE_OAUTH_CLIENT_JSON", jsonPath)

	clientID, clientSecret := gmailOAuthCredentials()
	if clientID != "test-client.apps.googleusercontent.com" {
		t.Fatalf("clientID = %q", clientID)
	}
	if clientSecret != "test-secret" {
		t.Fatalf("clientSecret = %q", clientSecret)
	}
}

func TestStartGmailDesktopConnectUsesLoopbackRedirect(t *testing.T) {
	t.Setenv("GOOGLE_OAUTH_CLIENT_ID", "test-client.apps.googleusercontent.com")
	t.Setenv("GOOGLE_OAUTH_CLIENT_SECRET", "test-secret")
	t.Setenv("GOOGLE_OAUTH_CLIENT_JSON", "")

	response, err := startGmailDesktopConnect(filepath.Join(t.TempDir(), "openings_state.json"))
	if err != nil {
		t.Fatalf("startGmailDesktopConnect error: %v", err)
	}
	if !response.Ok {
		t.Fatalf("expected ok response")
	}
	parsed, err := url.Parse(response.AuthURL)
	if err != nil {
		t.Fatalf("parse auth url: %v", err)
	}
	redirectURI := parsed.Query().Get("redirect_uri")
	if !strings.HasPrefix(redirectURI, "http://127.0.0.1:") {
		t.Fatalf("redirect uri = %q", redirectURI)
	}
	if parsed.Query().Get("client_id") != "test-client.apps.googleusercontent.com" {
		t.Fatalf("unexpected client id in auth url")
	}
	callbackURL := redirectURI + "/?state=" + url.QueryEscape(parsed.Query().Get("state")) + "&error=access_denied"
	resp, err := http.Get(callbackURL)
	if err != nil {
		t.Fatalf("trigger loopback callback: %v", err)
	}
	_ = resp.Body.Close()
}

func TestMailDBReadWaitsDuringExclusiveWrite(t *testing.T) {
	tempDir := t.TempDir()
	statePath := filepath.Join(tempDir, "openings_state.json")
	t.Setenv("MAIL_DB_PATH", filepath.Join(tempDir, "mail.db"))

	account, err := upsertMailAccount(statePath, mailStoredAccount{
		MailAccount: MailAccount{
			Provider:    "outlook",
			Email:       "candidate@example.com",
			DisplayName: "Candidate",
			Status:      "connected",
			Scopes:      []string{"Mail.Read"},
			ConnectedAt: utcNow(),
		},
		TokenJSON: `{"kind":"outlook_web","storage_state_path":"state.json"}`,
	})
	if err != nil {
		t.Fatalf("upsertMailAccount error: %v", err)
	}
	if _, _, err := upsertMailMessages(statePath, mailStoredAccount{MailAccount: account}, []MailMessage{
		{
			Provider:          "outlook",
			ProviderMessageID: "msg-1",
			ProviderThreadID:  "thread-1",
			Subject:           "Interview confirmation",
			Sender:            MailAddress{Name: "Recruiter", Email: "recruiter@example.com"},
			ReceivedAt:        utcNow(),
			Snippet:           "Interview confirmed.",
			BodyText:          "Interview confirmed for tomorrow.",
			EventType:         mailEventInterviewScheduled,
			Importance:        true,
			Confidence:        0.9,
			DecisionSource:    "rules",
			TriageStatus:      mailTriageNew,
		},
	}); err != nil {
		t.Fatalf("upsertMailMessages error: %v", err)
	}

	db, err := openMailDB(statePath)
	if err != nil {
		t.Fatalf("openMailDB error: %v", err)
	}
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatalf("db.Conn error: %v", err)
	}
	if _, err := conn.ExecContext(context.Background(), `BEGIN EXCLUSIVE`); err != nil {
		_ = conn.Close()
		t.Fatalf("BEGIN EXCLUSIVE error: %v", err)
	}

	resultCh := make(chan error, 1)
	go func() {
		_, loadErr := loadMailMessages(statePath, MailMessageFilters{UnreadOnly: true, ImportantOnly: true, Limit: 100})
		resultCh <- loadErr
	}()

	time.Sleep(150 * time.Millisecond)
	if _, err := conn.ExecContext(context.Background(), `COMMIT`); err != nil {
		_ = conn.Close()
		t.Fatalf("COMMIT error: %v", err)
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("conn.Close error: %v", err)
	}

	select {
	case loadErr := <-resultCh:
		if loadErr != nil {
			t.Fatalf("loadMailMessages error: %v", loadErr)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for loadMailMessages to finish after write lock released")
	}
}

func TestMailDBPendingUpsertDoesNotWipeHydratedBody(t *testing.T) {
	tempDir := t.TempDir()
	statePath := filepath.Join(tempDir, "openings_state.json")
	t.Setenv("MAIL_DB_PATH", filepath.Join(tempDir, "mail.db"))

	account, err := upsertMailAccount(statePath, mailStoredAccount{
		MailAccount: MailAccount{
			Provider:    string(MailProviderGmail),
			Email:       "candidate@example.com",
			DisplayName: "Candidate",
			Status:      "connected",
			Scopes:      []string{"gmail.readonly"},
			ConnectedAt: utcNow(),
		},
		TokenJSON: `{"access_token":"token"}`,
	})
	if err != nil {
		t.Fatalf("upsertMailAccount error: %v", err)
	}

	fullMessage := MailMessage{
		Provider:          string(MailProviderGmail),
		ProviderMessageID: "msg-1",
		ProviderThreadID:  "thread-1",
		Subject:           "Interview confirmation",
		Sender:            MailAddress{Name: "Recruiter", Email: "recruiter@example.com"},
		ReceivedAt:        utcNow(),
		Snippet:           "Interview confirmed.",
		BodyText:          "Interview confirmed for tomorrow.",
		BodyHTML:          "<p>Interview confirmed for tomorrow.</p>",
		HydrationStatus:   mailHydrationComplete,
		MetadataSource:    mailMetadataSourceLegacy,
		HydratedAt:        utcNow(),
		EventType:         mailEventInterviewScheduled,
		Importance:        true,
		Confidence:        0.96,
		DecisionSource:    "rules",
		TriageStatus:      mailTriageNew,
	}
	if _, _, err := upsertMailMessages(statePath, mailStoredAccount{MailAccount: account}, []MailMessage{fullMessage}); err != nil {
		t.Fatalf("initial upsertMailMessages error: %v", err)
	}

	pendingRow := MailMessage{
		Provider:          string(MailProviderGmail),
		ProviderMessageID: "msg-1",
		ProviderThreadID:  "thread-1",
		Subject:           "Interview confirmation",
		Sender:            MailAddress{Name: "Recruiter", Email: "recruiter@example.com"},
		ReceivedAt:        fullMessage.ReceivedAt,
		Snippet:           "Interview confirmed.",
		HydrationStatus:   mailHydrationPending,
		MetadataSource:    mailMetadataSourceLegacy,
		EventType:         mailEventInterviewScheduled,
		Importance:        true,
		Confidence:        0.9,
		DecisionSource:    "rules",
		TriageStatus:      mailTriageNew,
	}
	if _, _, err := upsertMailMessages(statePath, mailStoredAccount{MailAccount: account}, []MailMessage{pendingRow}); err != nil {
		t.Fatalf("pending upsertMailMessages error: %v", err)
	}

	list, err := loadMailMessages(statePath, MailMessageFilters{Limit: 10})
	if err != nil {
		t.Fatalf("loadMailMessages error: %v", err)
	}
	if len(list.Messages) != 1 {
		t.Fatalf("messages len = %d, want 1", len(list.Messages))
	}
	if list.Messages[0].HydrationStatus != mailHydrationComplete {
		t.Fatalf("hydration status = %q, want %q", list.Messages[0].HydrationStatus, mailHydrationComplete)
	}

	detail, err := loadMailMessageDetail(statePath, list.Messages[0].ID)
	if err != nil {
		t.Fatalf("loadMailMessageDetail error: %v", err)
	}
	if detail.BodyText != fullMessage.BodyText {
		t.Fatalf("body text = %q, want %q", detail.BodyText, fullMessage.BodyText)
	}
	if detail.BodyHTML != fullMessage.BodyHTML {
		t.Fatalf("body html = %q, want %q", detail.BodyHTML, fullMessage.BodyHTML)
	}
}

func TestLiveMailViewsExcludeHistoricalOutlookRows(t *testing.T) {
	tempDir := t.TempDir()
	statePath := filepath.Join(tempDir, "openings_state.json")
	t.Setenv("MAIL_DB_PATH", filepath.Join(tempDir, "mail.db"))

	account, err := upsertMailAccount(statePath, mailStoredAccount{
		MailAccount: MailAccount{
			Provider:    string(MailProviderOutlook),
			Email:       "student@northeastern.edu",
			DisplayName: "Student Outlook",
			Status:      "connected",
			Scopes:      []string{"outlook_web_session"},
			ConnectedAt: utcNow(),
		},
		TokenJSON: `{"kind":"outlook_web","storage_state_path":"state.json"}`,
	})
	if err != nil {
		t.Fatalf("upsertMailAccount error: %v", err)
	}
	if _, _, err := upsertMailMessages(statePath, mailStoredAccount{MailAccount: account}, []MailMessage{
		{
			Provider:          string(MailProviderOutlook),
			ProviderMessageID: "outlook-only-1",
			ProviderThreadID:  "thread-1",
			Subject:           "Application received",
			Sender:            MailAddress{Name: "OpenAI Careers", Email: "noreply@openai.com"},
			ReceivedAt:        utcNow(),
			Snippet:           "Thank you for applying.",
			BodyText:          "Thank you for applying.",
			HydrationStatus:   mailHydrationComplete,
			MetadataSource:    mailMetadataSourceOWAService,
			EventType:         mailEventApplicationAcknowledged,
			TriageStatus:      mailTriageNew,
			Importance:        true,
			Confidence:        0.95,
			DecisionSource:    "historical",
		},
	}); err != nil {
		t.Fatalf("upsertMailMessages error: %v", err)
	}

	list, err := loadMailMessages(statePath, MailMessageFilters{Limit: 10})
	if err != nil {
		t.Fatalf("loadMailMessages error: %v", err)
	}
	if len(list.Messages) != 0 {
		t.Fatalf("messages len = %d, want 0", len(list.Messages))
	}
	if list.Summary.TotalMessages != 0 || list.Summary.ConnectedAccounts != 0 {
		t.Fatalf("unexpected live summary: %#v", list.Summary)
	}

	overview, err := loadMailOverview(statePath)
	if err != nil {
		t.Fatalf("loadMailOverview error: %v", err)
	}
	if len(overview.Accounts) != 0 {
		t.Fatalf("overview accounts len = %d, want 0", len(overview.Accounts))
	}
	if overview.Summary.NewMessageCount != 0 || overview.Summary.ConnectedAccountsCount != 0 {
		t.Fatalf("unexpected overview summary: %#v", overview.Summary)
	}

	analytics, err := loadMailAnalytics(statePath, MailMessageFilters{Limit: 25})
	if err != nil {
		t.Fatalf("loadMailAnalytics error: %v", err)
	}
	if analytics.Summary.ConnectedAccountsCount != 0 {
		t.Fatalf("connected accounts count = %d, want 0", analytics.Summary.ConnectedAccountsCount)
	}
	if analytics.Summary.AllTime.Applications != 0 {
		t.Fatalf("all-time applications = %d, want 0", analytics.Summary.AllTime.Applications)
	}
}

func TestLoadMailMessagesDedupesRedirectedGmailCopy(t *testing.T) {
	tempDir := t.TempDir()
	statePath := filepath.Join(tempDir, "openings_state.json")
	t.Setenv("MAIL_DB_PATH", filepath.Join(tempDir, "mail.db"))

	outlookAccount, err := upsertMailAccount(statePath, mailStoredAccount{
		MailAccount: MailAccount{
			Provider:    string(MailProviderOutlook),
			Email:       "student@northeastern.edu",
			DisplayName: "Student Outlook",
			Status:      "connected",
			Scopes:      []string{"outlook_web_session"},
			ConnectedAt: utcNow(),
		},
		TokenJSON: `{"kind":"outlook_web","storage_state_path":"state.json"}`,
	})
	if err != nil {
		t.Fatalf("upsertMailAccount outlook error: %v", err)
	}
	gmailAccount, err := upsertMailAccount(statePath, mailStoredAccount{
		MailAccount: MailAccount{
			Provider:    string(MailProviderGmail),
			Email:       "candidate@gmail.com",
			DisplayName: "Candidate Gmail",
			Status:      "connected",
			Scopes:      []string{"gmail.readonly"},
			ConnectedAt: utcNow(),
		},
		TokenJSON: `{"access_token":"token"}`,
	})
	if err != nil {
		t.Fatalf("upsertMailAccount gmail error: %v", err)
	}

	receivedAt := time.Now().In(time.Local).Add(-90 * time.Minute).Format(time.RFC3339)
	internetMessageID := "<redirected-application@example.com>"

	if _, _, err := upsertMailMessages(statePath, mailStoredAccount{MailAccount: outlookAccount}, []MailMessage{
		{
			Provider:          string(MailProviderOutlook),
			ProviderMessageID: "outlook-msg-1",
			ProviderThreadID:  "outlook-thread-1",
			InternetMessageID: internetMessageID,
			Subject:           "Application received for Platform Engineer",
			Sender:            MailAddress{Name: "OpenAI Careers", Email: "noreply@openai.com"},
			ReceivedAt:        receivedAt,
			Snippet:           "Thanks for applying to OpenAI.",
			HydrationStatus:   mailHydrationPending,
			MetadataSource:    mailMetadataSourceOWAService,
			MatchedCompany:    "OpenAI",
			MatchedJobTitle:   "Platform Engineer",
			EventType:         mailEventApplicationAcknowledged,
			Confidence:        0.82,
			DecisionSource:    "rules",
			TriageStatus:      mailTriageNew,
			IsUnread:          true,
		},
	}); err != nil {
		t.Fatalf("upsertMailMessages outlook error: %v", err)
	}

	gmailBody := "Thank you for applying to the Platform Engineer role at OpenAI. We received your application."
	if _, _, err := upsertMailMessages(statePath, mailStoredAccount{MailAccount: gmailAccount}, []MailMessage{
		{
			Provider:          string(MailProviderGmail),
			ProviderMessageID: "gmail-msg-1",
			ProviderThreadID:  "gmail-thread-1",
			InternetMessageID: internetMessageID,
			Subject:           "Application received for Platform Engineer",
			Sender:            MailAddress{Name: "OpenAI Careers", Email: "noreply@openai.com"},
			ReceivedAt:        receivedAt,
			Snippet:           "Thank you for applying.",
			BodyText:          gmailBody,
			HydrationStatus:   mailHydrationComplete,
			MetadataSource:    mailMetadataSourceLegacy,
			MatchedCompany:    "OpenAI",
			MatchedJobTitle:   "Platform Engineer",
			EventType:         mailEventApplicationAcknowledged,
			Confidence:        0.96,
			DecisionSource:    "rules",
			TriageStatus:      mailTriageNew,
			IsUnread:          true,
		},
	}); err != nil {
		t.Fatalf("upsertMailMessages gmail error: %v", err)
	}

	list, err := loadMailMessages(statePath, MailMessageFilters{Limit: 10})
	if err != nil {
		t.Fatalf("loadMailMessages error: %v", err)
	}
	if len(list.Messages) != 1 {
		t.Fatalf("messages len = %d, want 1", len(list.Messages))
	}
	message := list.Messages[0]
	if message.Provider != string(MailProviderGmail) {
		t.Fatalf("provider = %q, want %q", message.Provider, MailProviderGmail)
	}
	if message.AccountEmail != gmailAccount.Email {
		t.Fatalf("account email = %q, want %q", message.AccountEmail, gmailAccount.Email)
	}
	if message.InternetMessageID != internetMessageID {
		t.Fatalf("internet message id = %q, want %q", message.InternetMessageID, internetMessageID)
	}
	if message.HydrationStatus != mailHydrationComplete {
		t.Fatalf("hydration status = %q, want %q", message.HydrationStatus, mailHydrationComplete)
	}
	if list.Summary.FilteredMessages != 1 {
		t.Fatalf("filtered messages = %d, want 1", list.Summary.FilteredMessages)
	}

	detail, err := loadMailMessageDetail(statePath, message.ID)
	if err != nil {
		t.Fatalf("loadMailMessageDetail error: %v", err)
	}
	if detail.Provider != string(MailProviderGmail) {
		t.Fatalf("detail provider = %q, want %q", detail.Provider, MailProviderGmail)
	}
	if detail.BodyText != gmailBody {
		t.Fatalf("detail body text = %q, want %q", detail.BodyText, gmailBody)
	}
}

func TestLoadMailAnalyticsDedupesRedirectedGmailCopy(t *testing.T) {
	tempDir := t.TempDir()
	statePath := filepath.Join(tempDir, "openings_state.json")
	t.Setenv("MAIL_DB_PATH", filepath.Join(tempDir, "mail.db"))

	outlookAccount, err := upsertMailAccount(statePath, mailStoredAccount{
		MailAccount: MailAccount{
			Provider:    string(MailProviderOutlook),
			Email:       "student@northeastern.edu",
			DisplayName: "Student Outlook",
			Status:      "connected",
			Scopes:      []string{"outlook_web_session"},
			ConnectedAt: utcNow(),
		},
		TokenJSON: `{"kind":"outlook_web","storage_state_path":"state.json"}`,
	})
	if err != nil {
		t.Fatalf("upsertMailAccount outlook error: %v", err)
	}
	gmailAccount, err := upsertMailAccount(statePath, mailStoredAccount{
		MailAccount: MailAccount{
			Provider:    string(MailProviderGmail),
			Email:       "candidate@gmail.com",
			DisplayName: "Candidate Gmail",
			Status:      "connected",
			Scopes:      []string{"gmail.readonly"},
			ConnectedAt: utcNow(),
		},
		TokenJSON: `{"access_token":"token"}`,
	})
	if err != nil {
		t.Fatalf("upsertMailAccount gmail error: %v", err)
	}

	receivedAt := time.Now().In(time.Local).Add(-2 * time.Hour).Format(time.RFC3339)
	internetMessageID := "<redirected-lifecycle@example.com>"
	outlookMessage := MailMessage{
		Provider:          string(MailProviderOutlook),
		ProviderMessageID: "outlook-msg-2",
		ProviderThreadID:  "outlook-thread-2",
		InternetMessageID: internetMessageID,
		Subject:           "Application received for Machine Learning Engineer",
		Sender:            MailAddress{Name: "OpenAI Careers", Email: "noreply@openai.com"},
		ReceivedAt:        receivedAt,
		Snippet:           "Thanks for applying to OpenAI.",
		HydrationStatus:   mailHydrationPending,
		MetadataSource:    mailMetadataSourceOWAService,
		MatchedCompany:    "OpenAI",
		MatchedJobTitle:   "Machine Learning Engineer",
		EventType:         mailEventApplicationAcknowledged,
		Confidence:        0.81,
		DecisionSource:    "rules",
		TriageStatus:      mailTriageReviewed,
	}
	gmailMessage := outlookMessage
	gmailMessage.Provider = string(MailProviderGmail)
	gmailMessage.ProviderMessageID = "gmail-msg-2"
	gmailMessage.ProviderThreadID = "gmail-thread-2"
	gmailMessage.Snippet = "Thank you for applying."
	gmailMessage.BodyText = "Thank you for applying to the Machine Learning Engineer role at OpenAI."
	gmailMessage.HydrationStatus = mailHydrationComplete
	gmailMessage.MetadataSource = mailMetadataSourceLegacy
	gmailMessage.Confidence = 0.97

	if _, _, err := upsertMailMessages(statePath, mailStoredAccount{MailAccount: outlookAccount}, []MailMessage{outlookMessage}); err != nil {
		t.Fatalf("upsertMailMessages outlook error: %v", err)
	}
	if _, _, err := upsertMailMessages(statePath, mailStoredAccount{MailAccount: gmailAccount}, []MailMessage{gmailMessage}); err != nil {
		t.Fatalf("upsertMailMessages gmail error: %v", err)
	}

	analytics, err := loadMailAnalytics(statePath, MailMessageFilters{Limit: 25})
	if err != nil {
		t.Fatalf("loadMailAnalytics error: %v", err)
	}
	if analytics.Summary.AllTime.Applications != 1 {
		t.Fatalf("all-time applications = %d, want 1", analytics.Summary.AllTime.Applications)
	}
	if analytics.Summary.Last7Days.Applications != 1 {
		t.Fatalf("last-7-days applications = %d, want 1", analytics.Summary.Last7Days.Applications)
	}
	if analytics.Summary.OpenApplicationsCount != 1 {
		t.Fatalf("open applications = %d, want 1", analytics.Summary.OpenApplicationsCount)
	}
	if len(analytics.Details.OpenApplications) != 1 {
		t.Fatalf("open applications len = %d, want 1", len(analytics.Details.OpenApplications))
	}
	if analytics.Details.OpenApplications[0].ID == 0 {
		t.Fatalf("open application id missing: %#v", analytics.Details.OpenApplications[0])
	}
}

func TestLoadMailOverviewDedupesRedirectedGmailCopy(t *testing.T) {
	tempDir := t.TempDir()
	statePath := filepath.Join(tempDir, "openings_state.json")
	t.Setenv("MAIL_DB_PATH", filepath.Join(tempDir, "mail.db"))

	outlookAccount, err := upsertMailAccount(statePath, mailStoredAccount{
		MailAccount: MailAccount{
			Provider:    string(MailProviderOutlook),
			Email:       "student@northeastern.edu",
			DisplayName: "Student Outlook",
			Status:      "connected",
			Scopes:      []string{"outlook_web_session"},
			ConnectedAt: utcNow(),
		},
		TokenJSON: `{"kind":"outlook_web","storage_state_path":"state.json"}`,
	})
	if err != nil {
		t.Fatalf("upsertMailAccount outlook error: %v", err)
	}
	gmailAccount, err := upsertMailAccount(statePath, mailStoredAccount{
		MailAccount: MailAccount{
			Provider:    string(MailProviderGmail),
			Email:       "candidate@gmail.com",
			DisplayName: "Candidate Gmail",
			Status:      "connected",
			Scopes:      []string{"gmail.readonly"},
			ConnectedAt: utcNow(),
		},
		TokenJSON: `{"access_token":"token"}`,
	})
	if err != nil {
		t.Fatalf("upsertMailAccount gmail error: %v", err)
	}

	receivedAt := time.Now().In(time.Local).Add(-45 * time.Minute).Format(time.RFC3339)
	internetMessageID := "<redirected-recruiter@example.com>"
	baseMessage := MailMessage{
		Subject:           "Re: Machine Learning Engineer application",
		Sender:            MailAddress{Name: "Recruiter", Email: "recruiter@openai.com"},
		ReceivedAt:        receivedAt,
		InternetMessageID: internetMessageID,
		MatchedCompany:    "OpenAI",
		MatchedJobTitle:   "Machine Learning Engineer",
		EventType:         mailEventRecruiterReply,
		Importance:        true,
		Confidence:        0.94,
		DecisionSource:    "rules",
		TriageStatus:      mailTriageNew,
		IsUnread:          true,
	}
	outlookMessage := baseMessage
	outlookMessage.Provider = string(MailProviderOutlook)
	outlookMessage.ProviderMessageID = "outlook-msg-3"
	outlookMessage.ProviderThreadID = "outlook-thread-3"
	outlookMessage.Snippet = "Can we find time to chat?"
	outlookMessage.HydrationStatus = mailHydrationPending
	outlookMessage.MetadataSource = mailMetadataSourceOWAService

	gmailMessage := baseMessage
	gmailMessage.Provider = string(MailProviderGmail)
	gmailMessage.ProviderMessageID = "gmail-msg-3"
	gmailMessage.ProviderThreadID = "gmail-thread-3"
	gmailMessage.Snippet = "Can we find time to chat?"
	gmailMessage.BodyText = "Can we find time to chat this week about your application?"
	gmailMessage.HydrationStatus = mailHydrationComplete
	gmailMessage.MetadataSource = mailMetadataSourceLegacy

	if _, _, err := upsertMailMessages(statePath, mailStoredAccount{MailAccount: outlookAccount}, []MailMessage{outlookMessage}); err != nil {
		t.Fatalf("upsertMailMessages outlook error: %v", err)
	}
	if _, _, err := upsertMailMessages(statePath, mailStoredAccount{MailAccount: gmailAccount}, []MailMessage{gmailMessage}); err != nil {
		t.Fatalf("upsertMailMessages gmail error: %v", err)
	}

	overview, err := loadMailOverview(statePath)
	if err != nil {
		t.Fatalf("loadMailOverview error: %v", err)
	}
	if overview.Summary.UnreadImportantCount != 1 {
		t.Fatalf("unread important count = %d, want 1", overview.Summary.UnreadImportantCount)
	}
	if overview.Summary.NewMessageCount != 1 {
		t.Fatalf("new message count = %d, want 1", overview.Summary.NewMessageCount)
	}
	if overview.Summary.EventCounts[mailEventRecruiterReply] != 1 {
		t.Fatalf("recruiter reply count = %d, want 1", overview.Summary.EventCounts[mailEventRecruiterReply])
	}
	if overview.Summary.LatestRecruiterReply == nil {
		t.Fatal("expected latest recruiter reply highlight")
	}
	if overview.Summary.LatestRecruiterReply.Subject != baseMessage.Subject {
		t.Fatalf("latest recruiter reply subject = %q, want %q", overview.Summary.LatestRecruiterReply.Subject, baseMessage.Subject)
	}
}

func TestDecodeGmailRawMessageAcceptsCommonBase64Variants(t *testing.T) {
	t.Parallel()

	raw := []byte("From: Recruiter <recruiter@example.com>\r\nSubject: Test\r\n\r\nHello from Gmail")
	wrappedEncoding := base64.URLEncoding.EncodeToString(raw)
	wrappedEncoding = wrappedEncoding[:20] + "\n" + wrappedEncoding[20:]
	tests := []struct {
		name    string
		encoded string
	}{
		{name: "raw url", encoded: base64.RawURLEncoding.EncodeToString(raw)},
		{name: "url padded", encoded: base64.URLEncoding.EncodeToString(raw)},
		{name: "standard padded", encoded: base64.StdEncoding.EncodeToString(raw)},
		{name: "wrapped", encoded: wrappedEncoding},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			decoded, err := decodeGmailRawMessage(test.encoded)
			if err != nil {
				t.Fatalf("decodeGmailRawMessage error: %v", err)
			}
			if string(decoded) != string(raw) {
				t.Fatalf("decoded payload = %q, want %q", string(decoded), string(raw))
			}
		})
	}
}

func TestOpenMailDBArchivesHistoricalOutlookAccount(t *testing.T) {
	tempDir := t.TempDir()
	statePath := filepath.Join(tempDir, "openings_state.json")
	t.Setenv("MAIL_DB_PATH", filepath.Join(tempDir, "mail.db"))

	account, err := upsertMailAccount(statePath, mailStoredAccount{
		MailAccount: MailAccount{
			Provider:    string(MailProviderOutlook),
			Email:       "student@northeastern.edu",
			DisplayName: "Student Outlook",
			Status:      "connected",
			Scopes:      []string{"outlook_web_session"},
			ConnectedAt: utcNow(),
		},
		TokenJSON: `{"kind":"outlook_web","storage_state_path":"state.json"}`,
	})
	if err != nil {
		t.Fatalf("upsertMailAccount error: %v", err)
	}

	db, err := openMailDB(statePath)
	if err != nil {
		t.Fatalf("openMailDB error: %v", err)
	}
	stored, err := loadStoredMailAccountByProvider(db, string(MailProviderOutlook))
	if err != nil {
		t.Fatalf("loadStoredMailAccountByProvider error: %v", err)
	}
	if stored.Status != "disconnected" {
		t.Fatalf("stored status = %q, want %q", stored.Status, "disconnected")
	}
	if stored.TokenJSON != "" {
		t.Fatalf("token json = %q, want empty", stored.TokenJSON)
	}
	if len(stored.Scopes) != 0 {
		t.Fatalf("scopes = %#v, want empty", stored.Scopes)
	}
	if stored.Email != account.Email {
		t.Fatalf("email = %q, want %q", stored.Email, account.Email)
	}
}

func TestMailBackfillDaysDefaultsToSeven(t *testing.T) {
	t.Setenv("MAIL_BACKFILL_DAYS", "")
	if got := mailBackfillDays(); got != 7 {
		t.Fatalf("mailBackfillDays() = %d, want 7", got)
	}
}

func TestMailSyncRunnerMarkAccountProgressUpdatesTotals(t *testing.T) {
	runner := NewMailSyncRunner(MailServiceConfig{})
	runner.status = MailRunStatus{Running: true}
	account := mailStoredAccount{
		MailAccount: MailAccount{
			ID:       1,
			Provider: string(MailProviderGmail),
			Email:    "candidate@example.com",
			Status:   "connected",
		},
	}

	runner.markAccountStart(account)
	runner.markAccountProgress(account, MailSyncAccountSummary{
		AccountID:      account.ID,
		Provider:       account.Provider,
		AccountEmail:   account.Email,
		Phase:          "discovering",
		Fetched:        12,
		Stored:         9,
		Discovered:     12,
		Hydrated:       4,
		ImportantCount: 3,
		CutoffReached:  true,
		DegradedMode:   true,
	}, "Fetched 12 Gmail messages.")

	snapshot := runner.Snapshot()
	if snapshot.MessagesFetched != 12 || snapshot.MessagesStored != 9 || snapshot.MessagesDiscovered != 12 || snapshot.MessagesHydrated != 4 || snapshot.ImportantMessages != 3 {
		t.Fatalf(
			"unexpected live totals: fetched=%d stored=%d discovered=%d hydrated=%d important=%d",
			snapshot.MessagesFetched,
			snapshot.MessagesStored,
			snapshot.MessagesDiscovered,
			snapshot.MessagesHydrated,
			snapshot.ImportantMessages,
		)
	}
	if !snapshot.CutoffReached || !snapshot.DegradedMode {
		t.Fatalf("expected cutoff/degraded flags to be true: %+v", snapshot)
	}
	if len(snapshot.Progress) != 1 {
		t.Fatalf("progress entries = %d, want 1", len(snapshot.Progress))
	}
	entry := snapshot.Progress[0]
	if entry.Phase != "discovering" {
		t.Fatalf("progress phase = %q, want %q", entry.Phase, "discovering")
	}
	if entry.Message != "Fetched 12 Gmail messages." {
		t.Fatalf("progress message = %q", entry.Message)
	}
	if entry.Fetched != 12 || entry.Stored != 9 || entry.Discovered != 12 || entry.Hydrated != 4 || entry.Important != 3 {
		t.Fatalf("unexpected progress counts: %+v", entry)
	}
	if !entry.CutoffReached || !entry.DegradedMode {
		t.Fatalf("expected progress flags to be true: %+v", entry)
	}
}
