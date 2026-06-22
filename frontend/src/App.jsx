import React, { useState, useRef } from 'react';
import axios from 'axios';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import './index.css';

export default function App() {
  const [file, setFile] = useState(null);
  const [loading, setLoading] = useState(false);
  const [patientData, setPatientData] = useState(null);
  const [aiStream, setAiStream] = useState('');
  const [isStreaming, setIsStreaming] = useState(false);
  const [activeTab, setActiveTab] = useState('questions');
  const [notPatientDoc, setNotPatientDoc] = useState(false);
  const fileInputRef = useRef(null);

  const handleFileChange = (e) => {
    setFile(e.target.files[0]);
  };

  const handleUpload = async (e) => {
    e.preventDefault();
    if (!file) return alert('Please select a medical document first.');

    setLoading(true);
    setPatientData(null);
    setAiStream('');
    setNotPatientDoc(false);

    const formData = new FormData();
    formData.append('file', file);

    try {
      const response = await axios.post('http://localhost:8080/api/upload', formData, {
        headers: { 'Content-Type': 'multipart/form-data' },
      });

      const record = response.data;
      setPatientData(record);
      setLoading(false);

      setIsStreaming(true);
      const eventSource = new EventSource(`http://localhost:8080/api/specialist/stream?id=${record.id}`);

      eventSource.onmessage = (event) => {
        if (event.data === '[DONE]') {
          eventSource.close();
          setIsStreaming(false);
        } else {
          try {
            const parsed = JSON.parse(event.data);
            if (parsed.text) {
              setAiStream((prev) => prev + parsed.text);
            }
          } catch (e) {
            setAiStream((prev) => prev + event.data);
          }
        }
      };

      eventSource.onerror = (err) => {
        console.error('Stream error:', err);
        eventSource.close();
        setIsStreaming(false);
      };

    } catch (error) {
      setLoading(false);
      const serverMsg = error?.response?.data?.error;
      if (serverMsg === 'not_patient_doc') {
        setNotPatientDoc(true);
      } else {
        alert('Failed to process patient record. Make sure your Go server is running!');
      }
      console.error('Upload failed:', error);
    }
  };

  const scrollToUpload = () => {
    document.getElementById('upload-section').scrollIntoView({ behavior: 'smooth' });
  };

  // Parses the raw markdown stream robustly into 3 distinct sections
  const parseStream = (text) => {
    const sections = { questions: '', suggestions: '', tests: '' };
    let current = 'questions';

    const lines = text.split('\n');
    for (const line of lines) {
      if (/^\s*(#+|\*\*).*\bquestion/i.test(line) || /^\s*question/i.test(line) && line.length < 30) {
        current = 'questions';
        continue;
      } else if (/^\s*(#+|\*\*).*\bsuggestion/i.test(line) || /^\s*suggestion/i.test(line) && line.length < 30) {
        current = 'suggestions';
        continue;
      } else if (/^\s*(#+|\*\*).*\btest/i.test(line) || /^\s*test/i.test(line) && line.length < 30) {
        current = 'tests';
        continue;
      }
      sections[current] += line + '\n';
    }
    return sections;
  };

  const parsedData = parseStream(aiStream);

  const toggleTab = (tabName) => {
    if (activeTab === tabName) {
      setActiveTab(null); // Allow closing all
    } else {
      setActiveTab(tabName);
    }
  };

  return (
    <div style={{ maxWidth: '900px', margin: '0 auto', padding: '60px 24px 100px' }} className="center-content">

      {/* ── Header Area ── */}
      <header className="fade-in" style={{ marginBottom: '50px' }}>
        <h1 style={{ fontSize: '42px', marginBottom: '16px', letterSpacing: '-0.02em' }}>
          Sovereign Copilot
        </h1>
        <div style={{
          width: '60px', height: '4px', background: 'var(--text-accent)',
          margin: '0 auto 30px', borderRadius: '2px'
        }} />
        <p style={{
          fontSize: '18px', lineHeight: '1.8', color: 'var(--text-secondary)',
          maxWidth: '700px', margin: '0 auto', fontStyle: 'italic',
          fontFamily: "'Playfair Display', serif"
        }}>
          "The Sovereign Copilot is a localized, multi-agent framework designed specifically for high-privacy clinical environments. By utilizing a zero-trust local container architecture, patient data never touches public APIs."
        </p>
      </header>

      {/* ── Call to Action Button ── */}
      <div className="fade-in" style={{ animationDelay: '0.2s', marginBottom: '80px' }}>
        <button onClick={scrollToUpload} className="hero-btn">
          <span>Get Yourself Checked</span>
        </button>
      </div>

      {/* ── Upload Section ── */}
      <section id="upload-section" className="glass-panel fade-in" style={{ animationDelay: '0.4s', padding: '40px', width: '100%', marginBottom: '60px' }}>
        <h2 style={{ fontSize: '24px', marginBottom: '10px' }}>Patient Intake</h2>
        <p style={{ color: 'var(--text-secondary)', marginBottom: '30px', fontSize: '15px' }}>
          Securely upload a clinical PDF or text document to begin triage.
        </p>

        <form onSubmit={handleUpload}>
          <label className="file-upload-container">
            <svg width="32" height="32" viewBox="0 0 24 24" fill="none" stroke="var(--text-accent)" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
              <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"></path>
              <polyline points="14 2 14 8 20 8"></polyline>
              <line x1="12" y1="18" x2="12" y2="12"></line>
              <line x1="9" y1="15" x2="15" y2="15"></line>
            </svg>
            <div style={{ textAlign: 'center' }}>
              <div style={{ fontSize: '18px', fontWeight: '600', color: 'var(--text-primary)', marginBottom: '6px' }}>
                {file ? file.name : 'Select Clinical Record'}
              </div>
              <div style={{ fontSize: '13px', color: 'var(--text-secondary)' }}>
                PDF or TXT. All processing runs locally.
              </div>
            </div>
            <input type="file" accept=".pdf,.txt" onChange={handleFileChange} ref={fileInputRef} />
          </label>

          <div style={{ marginTop: '30px' }}>
            <button type="submit" disabled={loading || !file} className="hero-btn" style={{ padding: '12px 30px', fontSize: '14px' }}>
              <span>
                {loading
                  ? 'Extracting Vitals...'
                  : 'Process Document'
                }
              </span>
            </button>
          </div>
        </form>

        {/* Guardrail Error Banner */}
        {notPatientDoc && (
          <div style={{
            marginTop: '24px',
            padding: '18px 24px',
            background: 'rgba(211, 84, 0, 0.08)',
            border: '1px solid rgba(211, 84, 0, 0.3)',
            borderRadius: '12px',
            display: 'flex',
            alignItems: 'flex-start',
            gap: '14px',
          }}>
            <span style={{ fontSize: '22px', lineHeight: 1 }}>⚠️</span>
            <div style={{ textAlign: 'left' }}>
              <div style={{ fontWeight: 700, color: 'var(--text-primary)', marginBottom: '4px' }}>Not a Patient Document</div>
              <div style={{ fontSize: '14px', color: 'var(--text-secondary)', lineHeight: 1.6 }}>
                This document does not appear to be a patient medical record. Please upload a valid clinical document such as a consultation note, lab report, or discharge summary.
              </div>
              <button
                onClick={() => setNotPatientDoc(false)}
                style={{ marginTop: '10px', background: 'none', border: 'none', cursor: 'pointer', color: 'var(--text-accent)', fontWeight: 600, fontSize: '13px', padding: 0 }}
              >
                Dismiss ✕
              </button>
            </div>
          </div>
        )}
      </section>

      {/* ── Main Workspace ── */}
      {patientData && (
        <div style={{ width: '100%', display: 'flex', flexDirection: 'column', gap: '40px' }} className="fade-in">

          {/* ── Patient Identity Card ── */}
          <div className="glass-panel" style={{ padding: '40px' }}>
            <h2 style={{ fontSize: '32px', marginBottom: '30px' }}>
              {patientData.patient_name || 'Unknown Patient'}
            </h2>

            <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(140px, 1fr))', gap: '20px', marginBottom: '30px' }}>
              <div className="data-chip">
                <div className="label">Age</div>
                <div className="value">{patientData.age || '—'}</div>
              </div>
              <div className="data-chip">
                <div className="label">Gender</div>
                <div className="value">{patientData.gender || '—'}</div>
              </div>
              <div className="data-chip">
                <div className="label">Blood Group</div>
                <div className="value">{patientData.blood_group || '—'}</div>
              </div>
            </div>

            {patientData.vitals && (
              <div style={{ marginBottom: '20px' }}>
                <h3 style={{ fontSize: '13px', textTransform: 'uppercase', letterSpacing: '1px', color: 'var(--text-secondary)', marginBottom: '8px', textAlign: 'left' }}>
                  Recorded Vitals
                </h3>
                <div className="text-block">{patientData.vitals}</div>
              </div>
            )}

            {patientData.symptoms_and_history && (
              <div>
                <h3 style={{ fontSize: '13px', textTransform: 'uppercase', letterSpacing: '1px', color: 'var(--text-secondary)', marginBottom: '8px', textAlign: 'left' }}>
                  Clinical History
                </h3>
                <div className="text-block">{patientData.symptoms_and_history}</div>
              </div>
            )}
          </div>

          {/* ── Specialist Analysis Tabs ── */}
          <div style={{ marginTop: '20px' }}>
            <h2 style={{ fontSize: '24px', marginBottom: '24px' }}>
              Clinical Recommendations {isStreaming && <span className="cursor-blink" style={{ display: 'inline-block', width: '10px', height: '10px', background: 'var(--text-accent)', borderRadius: '50%', marginLeft: '10px', verticalAlign: 'middle', animation: 'blink 1s infinite' }} />}
            </h2>

            {/* Tab Controls */}
            <div style={{ display: 'flex', gap: '16px', justifyContent: 'center', marginBottom: '30px', flexWrap: 'wrap' }}>
              <button
                className={`tab-btn ${activeTab === 'questions' ? 'active' : ''}`}
                onClick={() => toggleTab('questions')}
              >
                Questions {activeTab === 'questions' ? '▲' : '▼'}
              </button>
              <button
                className={`tab-btn ${activeTab === 'suggestions' ? 'active' : ''}`}
                onClick={() => toggleTab('suggestions')}
              >
                Suggestions {activeTab === 'suggestions' ? '▲' : '▼'}
              </button>
              <button
                className={`tab-btn ${activeTab === 'tests' ? 'active' : ''}`}
                onClick={() => toggleTab('tests')}
              >
                Tests to Order {activeTab === 'tests' ? '▲' : '▼'}
              </button>
            </div>

            {/* Active Tab Content Panel */}
            {activeTab && (
              <div className="glass-panel fade-in" style={{ padding: '30px', minHeight: '150px' }}>
                <div className={`stream-terminal ${isStreaming ? 'cursor-blink' : ''}`} style={{ minHeight: 'auto', border: 'none', background: 'transparent', padding: '0', boxShadow: 'none' }}>
                  {aiStream ? (
                    <div className="markdown-body">
                      <ReactMarkdown remarkPlugins={[remarkGfm]}>
                        {parsedData[activeTab] || '*No data provided for this section yet...*'}
                      </ReactMarkdown>
                    </div>
                  ) : (
                    <span style={{ color: 'var(--text-secondary)', fontStyle: 'italic' }}>
                      Awaiting AI reasoning engine...
                    </span>
                  )}
                </div>
              </div>
            )}
          </div>

        </div>
      )}

    </div>
  );
}