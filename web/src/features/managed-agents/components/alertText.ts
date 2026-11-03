const HTML_DOCUMENT_PATTERN = /<!doctype\s+html|<html[\s>]|<\/html>|<body[\s>]|<\/body>/i;
const HTTP_STATUS_PREFIX = /^([1-5]\d{2})(?:\s|$)/;
const MAX_SUMMARY_LENGTH = 240;

export type AlertTextAnalysis = {
  detail: string | null;
  html: boolean;
  status: number | null;
  summary: string;
};

export function analyzeAlertText(value: string): AlertTextAnalysis | null {
  const text = value.trim();
  if (!text) {
    return null;
  }
  const html = HTML_DOCUMENT_PATTERN.test(text);
  const statusMatch = text.match(HTTP_STATUS_PREFIX);
  const exceedsSummary = text.length > MAX_SUMMARY_LENGTH;
  return {
    detail: html || exceedsSummary ? text : null,
    html,
    status: statusMatch ? Number(statusMatch[1]) : null,
    summary: html ? '' : summarize(text),
  };
}

function summarize(text: string) {
  if (text.length <= MAX_SUMMARY_LENGTH) {
    return text;
  }
  return `${text.slice(0, MAX_SUMMARY_LENGTH).trimEnd()}…`;
}
