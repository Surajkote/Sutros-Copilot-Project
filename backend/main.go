/*
Sovereign Copilot — Backend Engine

Architecture:
  - Gin HTTP server exposing a REST API on :8080
  - POST /api/upload receives a multipart/form-data file upload (e.g., PDF)
    and extracts text from it before routing it through Agent A (Scribe Agent).
  - Agent A calls the Groq LLM API with a strict system prompt to extract structured
    clinical data as JSON, which is persisted to Postgres via GORM.
  - After Postgres persistence, the raw clinical text is embedded (mock 1536-dim vector)
    and upserted into a Qdrant vector collection for semantic retrieval.
  - GET /api/specialist/stream fetches a saved PatientRecord by ID, retrieves the most
    semantically relevant clinical context from Qdrant, and routes both through
    Agent B (Specialist Agent), which streams follow-up questions and test
    recommendations back to the client as Server-Sent Events (SSE).
  - PatientRecord is the persistence model (Postgres via GORM) and is kept separate from the
    extraction DTO so the two concerns can evolve independently.
*/
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strings"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"github.com/ledongthuc/pdf"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

var DB *gorm.DB

// ErrNotPatientDoc is returned by RunScribeAgent when the uploaded document is not a patient medical record.
var ErrNotPatientDoc = errors.New("not_patient_doc")

// PatientRecord is the Postgres persistence model for a patient encounter.
type PatientRecord struct {
	ID                 uint   `json:"id"                gorm:"primaryKey"`
	PatientName        string `json:"patient_name"`
	Age                int    `json:"age"`
	Gender             string `json:"gender"`
	BloodGroup         string `json:"blood_group"`
	Vitals             string `json:"vitals"`
	SymptomsAndHistory string `json:"symptoms_and_history"`
	RawExtractedText   string `json:"raw_extracted_text"`
}

// ScribeExtraction is the structured clinical data returned by Agent A.
type ScribeExtraction struct {
	PatientName        string `json:"patient_name"`
	Age                int    `json:"age"`
	Gender             string `json:"gender"`
	BloodGroup         string `json:"blood_group"`
	Vitals             string `json:"vitals"`
	SymptomsAndHistory string `json:"symptoms_and_history"`
}

const groqEndpoint = "https://api.groq.com/openai/v1/chat/completions"
const qdrantEndpoint = "http://qdrant:6333"
const scribeModel = "openai/gpt-oss-20b"
const specialistModel = "openai/gpt-oss-120b"

const specialistSystemPrompt = `You are a Specialist Medical AI. You MUST output your response under EXACTLY these three Markdown headers, in this order:
### Questions
### Suggestions
### Tests

For EACH section, you MUST produce a Markdown table with exactly two columns.
Do NOT use bullet points or numbered lists anywhere. Tables only.

Under "### Questions", output a table with columns: | Question | Clinical Rationale |
Under "### Suggestions", output a table with columns: | Suggestion | Citation from Patient Record |
For EVERY row in Suggestions, the Citation column MUST contain an exact quote from the patient's raw text formatted as: [Citation: 'exact words from text']. This is MANDATORY. If a row has no citation, it is wrong.
Under "### Tests", output a table with columns: | Test to Order | Reason / Clinical Justification |`

const scribeSystemPrompt = `You are a clinical data extraction engine for a medical records system.

GUARDRAIL — DOCUMENT VALIDATION (check this first):
Before extracting any data, assess whether the document is a patient-related medical record. A valid patient document contains at least some of: patient name, age, symptoms, diagnoses, medications, vitals, lab results, clinical notes, or medical history.
If the document is clearly NOT a patient medical record — for example it is a resume, job application, legal contract, invoice, vehicle document, hotel booking, news article, or any other completely non-medical document — you MUST return ONLY this exact JSON and nothing else:
  {"not_patient_doc": true}

Your job (only if the document IS a valid patient record):
1. Read the raw input text, which may be a voice transcript, a scanned document, or a clinical note.
2. Ignore anything that is not medically relevant — casual conversation, opinions, unrelated topics, pleasantries, or noise.
3. A document may contain some irrelevant content alongside medical data — that is acceptable. Extract the medical data and ignore the rest.
4. Extract ONLY the following fields from the medically relevant content:
   - patient_name: Full name of the patient (string). Use "" if not found.
   - age: Patient's age as an integer. Use 0 if not found.
   - gender: Patient's gender (string, e.g., "Male", "Female", "Other"). Use "" if not found.
   - blood_group: ABO/Rh blood type (e.g., "A+", "O-"). Use "" if not found.
   - vitals: A concise summary of vital signs (e.g., "BP: 120/80 mmHg, HR: 72 bpm, Temp: 98.6°F"). Use "" if not found.
   - symptoms_and_history: A concise summary of medically relevant history, complaints, symptoms, or diagnoses. Use "" if not found.

STRICT OUTPUT RULES:
- Return ONLY a single, valid JSON object. No markdown, no explanation, no code fences, no extra text.
- The JSON must match exactly this structure:
  {"patient_name":"","age":0,"gender":"","blood_group":"","vitals":"","symptoms_and_history":""}
- Do not infer or hallucinate data that is not explicitly present in the input.`

// groqRequest mirrors the Groq /chat/completions request body.
type groqRequest struct {
	Model    string              `json:"model"`
	Messages []map[string]string `json:"messages"`
}

// groqStreamRequest mirrors the Groq /chat/completions request body with streaming enabled.
type groqStreamRequest struct {
	Model    string              `json:"model"`
	Messages []map[string]string `json:"messages"`
	Stream   bool                `json:"stream"`
}

// groqStreamChunk is one parsed SSE data chunk from the Groq streaming response.
type groqStreamChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
	} `json:"choices"`
}

// groqResponse mirrors the relevant fields of the Groq API response.
type groqResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// generateMockEmbedding returns a deterministic-length 1536-dim float32 vector.
// In production this would be replaced by a real embedding model call.
func generateMockEmbedding(text string) []float32 {
	vec := make([]float32, 1536)
	for i := range vec {
		vec[i] = rand.Float32()
	}
	return vec
}

// ensureQdrantCollection creates the patient_records collection if it does not exist.
func ensureQdrantCollection() {
	body, _ := json.Marshal(map[string]interface{}{
		"vectors": map[string]interface{}{
			"size":     1536,
			"distance": "Cosine",
		},
	})
	req, err := http.NewRequest(http.MethodPut, qdrantEndpoint+"/collections/patient_records", bytes.NewReader(body))
	if err != nil {
		log.Printf("qdrant: failed to build collection request: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("qdrant: collection ensure failed (is Qdrant running?): %v", err)
		return
	}
	resp.Body.Close()
}

// upsertQdrantPoint indexes a patient record's text as a vector point in Qdrant.
func upsertQdrantPoint(patientID uint, text string) {
	vector := generateMockEmbedding(text)
	body, err := json.Marshal(map[string]interface{}{
		"points": []map[string]interface{}{
			{
				"id":     fmt.Sprintf("%d", patientID),
				"vector": vector,
				"payload": map[string]interface{}{
					"patient_id": patientID,
					"text":       text,
				},
			},
		},
	})
	if err != nil {
		log.Printf("qdrant: failed to marshal point: %v", err)
		return
	}
	req, err := http.NewRequest(http.MethodPut, qdrantEndpoint+"/collections/patient_records/points", bytes.NewReader(body))
	if err != nil {
		log.Printf("qdrant: failed to build point request: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("qdrant: upsert failed: %v", err)
		return
	}
	resp.Body.Close()
}

// retrieveQdrantContext performs a filtered vector search and returns the top result's text.
func retrieveQdrantContext(patientID uint, queryText string) string {
	vector := generateMockEmbedding(queryText)
	body, err := json.Marshal(map[string]interface{}{
		"vector": vector,
		"limit":  1,
		"filter": map[string]interface{}{
			"must": []map[string]interface{}{
				{
					"key": "patient_id",
					"match": map[string]interface{}{
						"value": patientID,
					},
				},
			},
		},
		"with_payload": true,
	})
	if err != nil {
		log.Printf("qdrant: failed to marshal search request: %v", err)
		return ""
	}
	req, err := http.NewRequest(http.MethodPost, qdrantEndpoint+"/collections/patient_records/points/search", bytes.NewReader(body))
	if err != nil {
		log.Printf("qdrant: failed to build search request: %v", err)
		return ""
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("qdrant: search failed: %v", err)
		return ""
	}
	defer resp.Body.Close()
	var result struct {
		Result []struct {
			Payload struct {
				Text string `json:"text"`
			} `json:"payload"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		log.Printf("qdrant: failed to decode search response: %v", err)
		return ""
	}
	if len(result.Result) > 0 {
		return result.Result[0].Payload.Text
	}
	return ""
}

// RunScribeAgent sends rawText to the LLM and returns a parsed ScribeExtraction.
func RunScribeAgent(rawText string) (*ScribeExtraction, error) {
	apiKey := os.Getenv("GROQ_API_KEY")
	if apiKey == "" {
		return nil, errors.New("GROQ_API_KEY is not set")
	}

	payload := groqRequest{
		Model: scribeModel,
		Messages: []map[string]string{
			{"role": "system", "content": scribeSystemPrompt},
			{"role": "user", "content": rawText},
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, groqEndpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Groq request failed: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Groq returned %d: %s", resp.StatusCode, string(respBytes))
	}

	var orResp groqResponse
	if err := json.Unmarshal(respBytes, &orResp); err != nil {
		return nil, fmt.Errorf("failed to parse Groq response: %w", err)
	}

	if orResp.Error != nil {
		return nil, fmt.Errorf("Groq API error: %s", orResp.Error.Message)
	}

	if len(orResp.Choices) == 0 || orResp.Choices[0].Message.Content == "" {
		return nil, errors.New("Groq returned an empty response")
	}

	raw := strings.TrimSpace(orResp.Choices[0].Message.Content)

	// Guardrail: detect the sentinel value the model returns for non-medical docs.
	var guard struct {
		NotPatientDoc bool `json:"not_patient_doc"`
	}
	if err := json.Unmarshal([]byte(raw), &guard); err == nil && guard.NotPatientDoc {
		return nil, ErrNotPatientDoc
	}

	var extraction ScribeExtraction
	if err := json.Unmarshal([]byte(raw), &extraction); err != nil {
		return nil, fmt.Errorf("model returned non-JSON content: %w — raw: %s", err, raw)
	}

	return &extraction, nil
}

// initDB establishes the Postgres connection, auto-migrates the schema, and ensures the Qdrant collection.
func initDB() {
	dsn := "host=postgres user=medical_admin password=secure_password123 dbname=sovereign_health port=5432 sslmode=disable"
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}
	if err := db.AutoMigrate(&PatientRecord{}); err != nil {
		log.Fatalf("database migration failed: %v", err)
	}
	DB = db
	ensureQdrantCollection()
}

// StreamSpecialistAgent retrieves RAG context from Qdrant, then calls Groq with stream=true
// and forwards SSE chunks to the client.
func StreamSpecialistAgent(c *gin.Context, record PatientRecord) {
	apiKey := os.Getenv("GROQ_API_KEY")
	if apiKey == "" {
		c.SSEvent("error", "GROQ_API_KEY is not set")
		return
	}

	ragContext := retrieveQdrantContext(record.ID, record.SymptomsAndHistory)

	systemPrompt := specialistSystemPrompt
	if ragContext != "" {
		systemPrompt = fmt.Sprintf("%s\n\nRetrieved Clinical Context: %s", specialistSystemPrompt, ragContext)
	}

	userMessage := fmt.Sprintf(
		"Patient Vitals: %s\n\nFull Clinical Notes:\n%s",
		record.Vitals,
		record.RawExtractedText,
	)

	payload := groqStreamRequest{
		Model:  specialistModel,
		Stream: true,
		Messages: []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": userMessage},
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		c.SSEvent("error", fmt.Sprintf("failed to marshal request: %v", err))
		return
	}

	req, err := http.NewRequest(http.MethodPost, groqEndpoint, bytes.NewReader(body))
	if err != nil {
		c.SSEvent("error", fmt.Sprintf("failed to build request: %v", err))
		return
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.SSEvent("error", fmt.Sprintf("Groq request failed: %v", err))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBytes, _ := io.ReadAll(resp.Body)
		c.SSEvent("error", fmt.Sprintf("Groq returned %d: %s", resp.StatusCode, string(errBytes)))
		return
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			c.SSEvent("done", "")
			return
		}
		var chunk groqStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		if len(chunk.Choices) > 0 && chunk.Choices[0].Delta.Content != "" {
			c.SSEvent("message", map[string]string{"text": chunk.Choices[0].Delta.Content})
		}
	}
	if err := scanner.Err(); err != nil {
		c.SSEvent("error", fmt.Sprintf("stream read error: %v", err))
	}
}

// extractTextFromFile auto-detects whether the file is a true PDF or plain text
// by inspecting the first 5 bytes for the %PDF- header, and extracts accordingly.
func extractTextFromFile(path string) (string, error) {
	header := make([]byte, 5)
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("cannot open file: %w", err)
	}
	_, err = f.Read(header)
	f.Close()
	if err != nil {
		return "", fmt.Errorf("cannot read file header: %w", err)
	}

	if string(header) == "%PDF-" {
		pf, r, err := pdf.Open(path)
		if err != nil {
			return "", fmt.Errorf("failed to open PDF: %w", err)
		}
		defer pf.Close()

		var buf bytes.Buffer
		b, err := r.GetPlainText()
		if err != nil {
			return "", fmt.Errorf("failed to extract PDF text: %w", err)
		}
		buf.ReadFrom(b)
		return buf.String(), nil
	}

	rawBytes, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read text file: %w", err)
	}
	return string(rawBytes), nil
}

func main() {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, relying on system environment variables")
	}

	initDB()

	r := gin.Default()
	config := cors.DefaultConfig()
	config.AllowAllOrigins = true // Allows React (5173) to read the response
	config.AllowHeaders = []string{"Origin", "Content-Length", "Content-Type", "Authorization"}
	r.Use(cors.New(config))

	r.GET("/api/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":  "healthy",
			"service": "Sovereign Copilot Engine",
		})
	})

	r.POST("/api/upload", func(c *gin.Context) {
		file, err := c.FormFile("file")
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "file upload is required and must be under the 'file' key"})
			return
		}

		tempFile, err := os.CreateTemp("", "upload-*")
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create temporary file"})
			return
		}
		tempFilePath := tempFile.Name()
		defer os.Remove(tempFilePath)

		src, err := file.Open()
		if err != nil {
			tempFile.Close()
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to open uploaded file: %v", err)})
			return
		}

		if _, err := io.Copy(tempFile, src); err != nil {
			src.Close()
			tempFile.Close()
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to write temp file: %v", err)})
			return
		}
		src.Close()
		tempFile.Close()

		rawText, err := extractTextFromFile(tempFilePath)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to extract text: %v", err)})
			return
		}

		if strings.TrimSpace(rawText) == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "extracted text is empty or unreadable"})
			return
		}

		extraction, err := RunScribeAgent(rawText)
		if err != nil {
			if errors.Is(err, ErrNotPatientDoc) {
				c.JSON(http.StatusUnprocessableEntity, gin.H{
					"error":   "not_patient_doc",
					"message": "This document does not appear to be a patient medical record. Please upload a valid clinical document such as a consultation note, lab report, or discharge summary.",
				})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		record := PatientRecord{
			PatientName:        extraction.PatientName,
			Age:                extraction.Age,
			Gender:             extraction.Gender,
			BloodGroup:         extraction.BloodGroup,
			Vitals:             extraction.Vitals,
			SymptomsAndHistory: extraction.SymptomsAndHistory,
			RawExtractedText:   rawText,
		}

		if result := DB.Create(&record); result.Error != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to save patient record: %v", result.Error)})
			return
		}

		go upsertQdrantPoint(record.ID, record.RawExtractedText)

		c.JSON(http.StatusOK, record)
	})

	r.GET("/api/specialist/stream", func(c *gin.Context) {
		idParam := c.Query("id")
		if idParam == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "id query parameter is required"})
			return
		}

		var record PatientRecord
		if result := DB.First(&record, idParam); result.Error != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("patient record %s not found", idParam)})
			return
		}

		c.Header("Content-Type", "text/event-stream")
		c.Header("Cache-Control", "no-cache")
		c.Header("Connection", "keep-alive")
		c.Header("X-Accel-Buffering", "no")

		c.Stream(func(w io.Writer) bool {
			StreamSpecialistAgent(c, record)
			return false
		})
	})

	r.Run(":8080")
}
