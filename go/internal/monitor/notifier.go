package monitor

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"os"
	"sort"
	"strings"
	"time"
)

func BuildEmailContent(newJobs []Job, blocked []CrawlOutcome) (string, string) {
	subject := fmt.Sprintf("[Career Monitor] %d new opening(s) | %d blocked target(s) - %s", len(newJobs), len(blocked), localNow().Format("2006-01-02 15:04"))
	lines := []string{
		"Career monitor update.",
		"",
		fmt.Sprintf("Total new openings: %d", len(newJobs)),
		fmt.Sprintf("Blocked targets: %d", len(blocked)),
		"",
	}

	if len(newJobs) > 0 {
		lines = append(lines, "New openings", "")
		grouped := groupedJobs(newJobs)
		names := sortedCompanyNames(grouped)
		for _, company := range names {
			jobs := grouped[company]
			lines = append(lines, fmt.Sprintf("%s (%d)", company, len(jobs)))
			for _, job := range jobs {
				extras := make([]string, 0, 2)
				if strings.TrimSpace(job.Team) != "" {
					extras = append(extras, job.Team)
				}
				if strings.TrimSpace(job.Location) != "" {
					extras = append(extras, job.Location)
				}
				suffix := ""
				if len(extras) > 0 {
					suffix = " [" + strings.Join(extras, ", ") + "]"
				}
				lines = append(lines,
					"- "+job.Title+suffix,
					"  "+job.URL,
				)
			}
			lines = append(lines, "")
		}
	}

	if len(blocked) > 0 {
		lines = append(lines, "Blocked targets", "")
		sortedBlocked := make([]CrawlOutcome, len(blocked))
		copy(sortedBlocked, blocked)
		sort.Slice(sortedBlocked, func(i, j int) bool {
			return strings.ToLower(sortedBlocked[i].Company) < strings.ToLower(sortedBlocked[j].Company)
		})
		for _, outcome := range sortedBlocked {
			chain := strings.Join(outcome.AttemptedSources, " -> ")
			lines = append(lines,
				fmt.Sprintf("- %s (%s)", outcome.Company, chain),
				"  "+outcome.Message,
			)
		}
		lines = append(lines, "")
	}

	body := strings.TrimSpace(strings.Join(lines, "\n")) + "\n"
	return subject, body
}

func SendEmail(subject string, body string) error {
	smtpHost := strings.TrimSpace(os.Getenv("SMTP_HOST"))
	smtpPort := strings.TrimSpace(os.Getenv("SMTP_PORT"))
	if smtpPort == "" {
		smtpPort = "587"
	}
	smtpUser := strings.TrimSpace(os.Getenv("SMTP_USER"))
	smtpPass := strings.TrimSpace(os.Getenv("SMTP_PASS"))
	emailFrom := strings.TrimSpace(os.Getenv("EMAIL_FROM"))
	if emailFrom == "" {
		emailFrom = smtpUser
	}
	recipients := splitRecipients(os.Getenv("EMAIL_TO"))
	useTLS := parseBoolEnv("SMTP_USE_TLS", true)

	missing := make([]string, 0)
	if smtpHost == "" {
		missing = append(missing, "SMTP_HOST")
	}
	if smtpUser == "" {
		missing = append(missing, "SMTP_USER")
	}
	if smtpPass == "" {
		missing = append(missing, "SMTP_PASS")
	}
	if emailFrom == "" {
		missing = append(missing, "EMAIL_FROM")
	}
	if len(recipients) == 0 {
		missing = append(missing, "EMAIL_TO")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing email configuration: %s", strings.Join(missing, ", "))
	}

	message := bytes.Buffer{}
	message.WriteString("From: " + emailFrom + "\r\n")
	message.WriteString("To: " + strings.Join(recipients, ", ") + "\r\n")
	message.WriteString("Subject: " + subject + "\r\n")
	message.WriteString("Date: " + time.Now().Format(time.RFC1123Z) + "\r\n")
	message.WriteString("MIME-Version: 1.0\r\n")
	message.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	message.WriteString("\r\n")
	message.WriteString(body)

	serverAddr := net.JoinHostPort(smtpHost, smtpPort)
	auth := smtp.PlainAuth("", smtpUser, smtpPass, smtpHost)

	if useTLS {
		conn, err := net.Dial("tcp", serverAddr)
		if err != nil {
			return err
		}
		defer conn.Close()
		client, err := smtp.NewClient(conn, smtpHost)
		if err != nil {
			return err
		}
		defer client.Close()
		if err := client.StartTLS(&tls.Config{ServerName: smtpHost}); err != nil {
			return err
		}
		if err := client.Auth(auth); err != nil {
			return err
		}
		if err := client.Mail(emailFrom); err != nil {
			return err
		}
		for _, rcpt := range recipients {
			if err := client.Rcpt(rcpt); err != nil {
				return err
			}
		}
		writer, err := client.Data()
		if err != nil {
			return err
		}
		if _, err := writer.Write(message.Bytes()); err != nil {
			return err
		}
		if err := writer.Close(); err != nil {
			return err
		}
		return client.Quit()
	}

	tlsConn, err := tls.Dial("tcp", serverAddr, &tls.Config{ServerName: smtpHost})
	if err != nil {
		return err
	}
	defer tlsConn.Close()
	client, err := smtp.NewClient(tlsConn, smtpHost)
	if err != nil {
		return err
	}
	defer client.Close()
	if err := client.Auth(auth); err != nil {
		return err
	}
	if err := client.Mail(emailFrom); err != nil {
		return err
	}
	for _, rcpt := range recipients {
		if err := client.Rcpt(rcpt); err != nil {
			return err
		}
	}
	writer, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := writer.Write(message.Bytes()); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	return client.Quit()
}
