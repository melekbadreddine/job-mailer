# Cold-Email Automator

A lightweight Go CLI application that utilizes the **Gemini 2.5 Flash API** to automate personalized job application outreach. The pipeline parses target company data from a CSV spreadsheet, blends it with your CV context, generates a targeted pitch, and sends a multi-part HTML email with a PDF attachment via Gmail SMTP.

## 🚀 Features

* **AI-Driven Personalization:** Generates custom 2-3 sentence pitches showing direct alignment with the target company's engineering stack.
* **State Recovery Engine:** Tracks progress via `progress.json`. If stopped, it automatically resumes where it left off, preventing duplicate emails.
* **Anti-Spam Safeguards:** Implements a strict 15-minute cool-down between successful deliveries to protect your email domain reputation.
* **Robust Error Handling:** Missing email addresses are routed to `missing_contacts_report.csv`, and API failures fallback safely to a pre-defined template.

## 📁 Repository Structure

```text
├── main.go                       # Core runtime pipeline and SMTP logic
├── template.html                 # Base HTML layout template
├── cv.md                         # Input CV markdown context file
├── CV_Melek_BADREDDINE.pdf       # Physical PDF attached to your emails
├── startups.csv                  # Input database spreadsheet containing targets
├── .env                          # Local workspace secret environment variables
└── .gitignore                    # Local environment exclusion guard

```

## ⚙️ Configuration (`.env`)

Create a `.env` file in the root directory:

```env
GMAIL_USER=your-email@gmail.com
GMAIL_PASSWORD=your-16-character-app-password
GEMINI_API_KEY=your-gemini-api-key

```

> **Note:** Generate your `GMAIL_PASSWORD` via Google Account Settings -> Security -> 2-Step Verification -> App Passwords. Do not use your standard password.

## 🏃 Run

1. **Install dependencies:**
```bash
go mod tidy

```

2. **Execute the automation script:**
```bash
go run main.go

```

3. **Monitor outbound deliveries live:**
```bash
tail -f delivery_report.log

```

4. **Reset execution progress (optional):**
```bash
rm progress.json

```