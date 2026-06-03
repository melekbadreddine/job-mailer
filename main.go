package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/smtp"
	"os"
	"strings"
	"time"

	"github.com/google/generative-ai-go/genai"
	"github.com/joho/godotenv"
	"google.golang.org/api/option"
)

type Startup struct {
	Company     string
	Sector      string
	Description string
	Website     string
	Contact     string
	Email       string
	Phone       string
}

type Progress struct {
	LastProcessedIndex int `json:"last_processed_index"`
}

const (
	progressFile      = "progress.json"
	reportFile        = "delivery_report.log"
	missingReportFile = "missing_contacts_report.csv"
	pdfFile           = "CV_Melek_BADREDDINE.pdf"
	templateFile      = "template.html"
)

func main() {
	// 1. Load configuration environment variables
	if err := godotenv.Load(); err != nil {
		fmt.Println("ℹ️ No .env file found, attempting to read system environment variables directly.")
	}

	gmailUser := os.Getenv("GMAIL_USER")
	gmailPass := os.Getenv("GMAIL_PASSWORD")
	geminiKey := os.Getenv("GEMINI_API_KEY")

	if gmailUser == "" || gmailPass == "" || geminiKey == "" {
		fmt.Println("❌ Error: Missing required environment variables (GMAIL_USER, GMAIL_PASSWORD, GEMINI_API_KEY) in .env file.")
		return
	}

	// 2. Read context CV files and the custom baseline HTML template
	cvBytes, err := os.ReadFile("cv.md")
	if err != nil {
		fmt.Printf("❌ Error reading cv.md: %v\n", err)
		return
	}
	cvContent := string(cvBytes)

	pdfBytes, err := os.ReadFile(pdfFile)
	if err != nil {
		fmt.Printf("❌ Error reading %s: %v. Please make sure the file exists.\n", pdfFile, err)
		return
	}

	rawTemplateBytes, err := os.ReadFile(templateFile)
	if err != nil {
		fmt.Printf("❌ Error reading base file %s: %v. Please create it first.\n", templateFile, err)
		return
	}
	baseHTMLTemplate := string(rawTemplateBytes)

	// 3. Handle state recovery memory
	lastIndex := loadProgress()
	if lastIndex > 0 {
		fmt.Printf("🔄 Resuming execution! Found previous progress memory. Skipping up to row index: %d\n", lastIndex)
	}

	// 4. Read target spreadsheet records
	csvFile, err := os.Open("startups.csv")
	if err != nil {
		fmt.Printf("❌ Error opening startups.csv: %v\n", err)
		return
	}
	defer csvFile.Close()

	records, err := csv.NewReader(csvFile).ReadAll()
	if err != nil {
		fmt.Printf("❌ Error parsing CSV file: %v\n", err)
		return
	}

	// 5. Initialize the Gemini API client utilizing modern v2.5 architecture
	ctx := context.Background()
	aiClient, err := genai.NewClient(ctx, option.WithAPIKey(geminiKey))
	if err != nil {
		fmt.Printf("❌ Error initializing Gemini client: %v\n", err)
		return
	}
	defer aiClient.Close()

	aiModel := aiClient.GenerativeModel("gemini-2.5-flash")
	aiModel.SetTemperature(0.5) 
	aiModel.SetMaxOutputTokens(2048)

	smtpHost := "smtp.gmail.com"
	smtpPort := "587"
	auth := smtp.PlainAuth("", gmailUser, gmailPass, smtpHost)

	firstEmailSentInThisRun := false

	// 6. Loop over CSV records
	for i, row := range records {
		if i == 0 {
			continue // Always skip headers
		}

		// State check: Skip rows we have already processed in a past run
		if i <= lastIndex {
			continue
		}

		// Row length safety verification
		if len(row) < 10 {
			continue
		}

		// Clean indices tracking: Correct mapping structure match
		target := Startup{
			Company:     strings.TrimSpace(row[1]),
			Sector:      strings.TrimSpace(row[2]),
			Website:     strings.TrimSpace(row[5]),
			Description: strings.TrimSpace(row[6]),
			Contact:     strings.TrimSpace(row[7]),
			Email:       strings.TrimSpace(row[8]),
			Phone:       strings.TrimSpace(row[9]),
		}

		// Filter step: Only look at IT companies
		if !isITSector(target.Sector) {
			fmt.Printf("Mesh Log (Row %d) ⏩ Skipping %s: Sector (%s) is outside target scope.\n", i, target.Company, target.Sector)
			saveProgress(i)
			continue
		}

		// Validation Step: Route missing email rows straight to the fallback CSV report
		if !isValidEmail(target.Email) {
			appendMissingContactReport(target)
			fmt.Printf("Mesh Log (Row %d) 📋 %s added to missing_contacts_report.csv (Missing Email)\n", i, target.Company)
			saveProgress(i)
			continue
		}

		// Anti-Spam throttle protection: Sleep 15 minutes between real deliveries
		if firstEmailSentInThisRun {
			fmt.Println("⏳ Cooling down for 15 minutes to mimic natural human typing behaviors and protect Gmail score...")
			time.Sleep(15 * time.Minute)
		}

		fmt.Printf("\n🤖 Row %d: Customizing context via Gemini API for %s...\n", i, target.Company)

		// 7. Leverage Gemini to write a tailored core alignment statement
		emailBody, err := generatePersonalizedEmail(ctx, aiModel, target, cvContent, baseHTMLTemplate)
		if err != nil {
			fmt.Printf("⚠️ Gemini unavailable for %s. Using fallback template. Error: %v\n", target.Company, err)
			emailBody = fallbackEmail(target, baseHTMLTemplate)
		}

		// 8. Construct multi-part message payloads matching corporate email protocol specifications
		subject := fmt.Sprintf("Candidature - Ingénieur Informatique - %s", target.Company)
		messagePayload := buildMultipartMessage(gmailUser, target.Email, subject, emailBody, pdfBytes, pdfFile)

		// 9. Execute Transmission utilizing the resilient retry handler
		err = sendMailWithRetry(smtpHost, smtpPort, auth, gmailUser, target.Email, messagePayload, 3, 10*time.Second)
		if err != nil {
			fmt.Printf("❌ Failed delivering mail to %s after multiple internal retries: %v\n", target.Company, err)
			continue
		}

		fmt.Printf("🚀 SUCCESS: Email cleanly injected into the system for %s (%s)!\n", target.Company, target.Email)
		firstEmailSentInThisRun = true

		// 10. Update persistent state memory data and append audits ONLY on actual successful deliveries
		saveProgress(i)
		logReport(target, emailBody)
	}

	fmt.Println("\n🏁 Finished processing all target rows in the spreadsheet data pool!")
}

func isITSector(sector string) bool {
	s := strings.ToLower(sector)
	keywords := []string{"software", "it", "devops", "cloud", "tech", "informatique", "génie logiciel", "qa", "data", "services"}
	for _, kw := range keywords {
		if strings.Contains(s, kw) {
			return true
		}
	}
	return false
}

func isValidEmail(email string) bool {
	email = strings.TrimSpace(strings.ToLower(email))
	invalid := map[string]bool{
		"":     true,
		"n.a.": true,
		"n/a":  true,
		"na":   true,
		"null": true,
		"-":    true,
		"none": true,
	}
	if invalid[email] {
		return false
	}
	return strings.Contains(email, "@")
}

func fallbackEmail(startup Startup, templateHTML string) string {
	fallbackParagraph := "Ingénieur en Informatique diplômé de l'ENIS, je possède des compétences en DevOps, Cloud Computing, Kubernetes, Terraform, automatisation et observabilité (LGTM stack) que je souhaite mettre au service de vos infrastructures."
	
	processedHTML := templateHTML
	processedHTML = strings.ReplaceAll(processedHTML, "{{.Company}}", startup.Company)
	processedHTML = strings.ReplaceAll(processedHTML, "{{.Sector}}", startup.Sector)
	processedHTML = strings.ReplaceAll(processedHTML, "{{.CustomParagraph}}", fallbackParagraph)
	return processedHTML
}

func appendMissingContactReport(s Startup) {
	fileExists := true
	if _, err := os.Stat(missingReportFile); os.IsNotExist(err) {
		fileExists = false
	}

	f, err := os.OpenFile(missingReportFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Printf("⚠️ Cannot write missing contacts report: %v\n", err)
		return
	}
	defer f.Close()

	writer := csv.NewWriter(f)
	defer writer.Flush()

	if !fileExists {
		_ = writer.Write([]string{
			"Company",
			"Sector",
			"Contact",
			"Email",
			"Phone",
			"Website",
			"Description",
		})
	}

	_ = writer.Write([]string{
		s.Company,
		s.Sector,
		s.Contact,
		s.Email,
		s.Phone,
		s.Website,
		s.Description,
	})
}

func generatePersonalizedEmail(ctx context.Context, model *genai.GenerativeModel, startup Startup, cv string, templateHTML string) (string, error) {
	prompt := fmt.Sprintf(`
Tu es un ingénieur informatique candidat qui s'adresse à une entreprise. Rédige uniquement **un paragraphe d'accroche personnalisé (2-3 phrases maximum)** en français montrant pourquoi tes compétences correspondent aux besoins de l'entreprise cible.

Instructions absolues de point de vue :
- Tu parles en ton nom propre ("Je", "Mon", "Mes").
- Tu t'adresses à l'entreprise en utilisant la deuxième personne du pluriel ("Votre expertise", "Vos solutions", "Votre pôle").
- Interdiction absolue d'utiliser "Notre", "Nos", "Nous" pour parler de l'entreprise cible (ex: Ne dis PAS "nos services de protection", dis plutôt "vos services de protection").

Instructions techniques :
- Si l'entreprise fait du Cloud/DevOps, mets l'accent sur tes compétences en architectures cloud-natives, observabilité (LGTM stack) et Kubernetes.
- Si elle fait du développement logiciel pur, insiste sur le génie logiciel, la clean architecture et les APIs.
- Ne renvoie AUCUN autre texte (pas de salutations, pas de signature, pas de blocs markdown). Renvoie uniquement le texte brut du paragraphe.

Détails de l'entreprise cible :
- Nom : %s
- Secteur : %s
- Description/Produit : %s

Mon CV (Contexte candidat) :
"""
%s
"""
`, startup.Company, startup.Sector, startup.Description, cv)

	resp, err := model.GenerateContent(ctx, genai.Text(prompt))
	if err != nil {
		return "", err
	}

	if len(resp.Candidates) == 0 || resp.Candidates[0].Content == nil {
		return "", fmt.Errorf("empty response received from Gemini model parameters")
	}

	var buf bytes.Buffer
	for _, part := range resp.Candidates[0].Content.Parts {
		fmt.Fprintf(&buf, "%v", part)
	}

	customParagraph := strings.TrimSpace(buf.String())
	
	// Inject variable context cleanly into the provided HTML blueprint
	processedHTML := templateHTML
	processedHTML = strings.ReplaceAll(processedHTML, "{{.Company}}", startup.Company)
	processedHTML = strings.ReplaceAll(processedHTML, "{{.Sector}}", startup.Sector)
	processedHTML = strings.ReplaceAll(processedHTML, "{{.CustomParagraph}}", customParagraph)

	return processedHTML, nil
}

func buildMultipartMessage(from, to, subject, bodyHTML string, attachmentBytes []byte, attachmentFilename string) []byte {
	markerBoundary := "DevOpsMimeSplitMarker777"

	var buf bytes.Buffer
	buf.WriteString(fmt.Sprintf("From: %s\r\n", from))
	buf.WriteString(fmt.Sprintf("To: %s\r\n", to))
	buf.WriteString(fmt.Sprintf("Subject: %s\r\n", subject))
	buf.WriteString("MIME-Version: 1.0\r\n")
	buf.WriteString(fmt.Sprintf("Content-Type: multipart/mixed; boundary=%s\r\n\r\n", markerBoundary))

	buf.WriteString(fmt.Sprintf("--%s\r\n", markerBoundary))
	buf.WriteString("Content-Type: text/html; charset=\"UTF-8\"\r\n")
	buf.WriteString("Content-Transfer-Encoding: 8bit\r\n\r\n")
	buf.WriteString(bodyHTML)
	buf.WriteString("\r\n\r\n")

	buf.WriteString(fmt.Sprintf("--%s\r\n", markerBoundary))
	buf.WriteString(fmt.Sprintf("Content-Type: application/pdf; name=\"%s\"\r\n", attachmentFilename))
	buf.WriteString("Content-Transfer-Encoding: base64\r\n")
	buf.WriteString(fmt.Sprintf("Content-Disposition: attachment; filename=\"%s\"\r\n\r\n", attachmentFilename))

	encoder := base64.NewEncoder(base64.StdEncoding, &buf)
	encoder.Write(attachmentBytes)
	encoder.Close()
	buf.WriteString("\r\n\r\n")

	buf.WriteString(fmt.Sprintf("--%s--\r\n", markerBoundary))

	return buf.Bytes()
}

func sendMailWithRetry(host, port string, auth smtp.Auth, from string, to string, msg []byte, maxRetries int, delay time.Duration) error {
	var err error
	for attempt := 1; attempt <= maxRetries; attempt++ {
		err = smtp.SendMail(host+":"+port, auth, from, []string{to}, msg)
		if err == nil {
			return nil
		}
		fmt.Printf("   ⚠️ Connection retry warn (%d/%d) sending to %s: %v\n", attempt, maxRetries, to, err)
		if attempt < maxRetries {
			time.Sleep(delay)
		}
	}
	return err
}

func loadProgress() int {
	if _, err := os.Stat(progressFile); os.IsNotExist(err) {
		return 0
	}
	data, err := os.ReadFile(progressFile)
	if err != nil {
		return 0
	}
	var p Progress
	if err := json.Unmarshal(data, &p); err != nil {
		return 0
	}
	return p.LastProcessedIndex
}

func saveProgress(index int) {
	p := Progress{LastProcessedIndex: index}
	data, _ := json.MarshalIndent(p, "", "  ")
	_ = os.WriteFile(progressFile, data, 0644)
}

func logReport(startup Startup, emailBody string) {
	f, err := os.OpenFile(reportFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Printf("⚠️ Internal execution warning: Failed appending analytics onto log tracker file: %v\n", err)
		return
	}
	defer f.Close()

	timestamp := time.Now().Format("2006-01-02 15:04:05")
	separator := strings.Repeat("-", 60)
	logEntry := fmt.Sprintf("[%s] COMPANY: %s | EMAIL: %s\n\nBODY GENERATED:\n%s\n%s\n\n", 
		timestamp, startup.Company, startup.Email, emailBody, separator)

	if _, err := f.WriteString(logEntry); err != nil {
		fmt.Printf("⚠️ Internal execution warning: Write fault parsing logger string: %v\n", err)
	}
}