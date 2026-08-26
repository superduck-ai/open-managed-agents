import { afterEach, beforeEach, describe, expect, test } from 'bun:test';
import { resetTestDom } from '../../../test/setup';
import { type ToolCallEntry } from '../types';
import {
  isAskUserQuestionCall,
  SessionRequiresActionCard,
  sessionToolConfirmationPublicId,
} from './SessionRequiresActionCard';

const testingLibrary = await import('@testing-library/react');
const { cleanup, fireEvent, render, screen, waitFor } = testingLibrary;

afterEach(() => {
  cleanup();
});

describe('SessionRequiresActionCard', () => {
  beforeEach(() => {
    resetTestDom('https://oma.duck.ai/workspaces/default/sessions/sesn_one123456');
  });

  const baseToolCall: ToolCallEntry = {
    id: 'tool_call-1',
    kind: 'tool_call',
    rawEventId: 'evt_bash_123',
    name: 'Bash',
    inputPreview: 'npm test',
    lifecycle: 'awaiting_approval',
    inferenceMs: 0,
    executionMs: 0,
    isError: false,
    bracketId: '',
    createdAtMs: Date.now(),
    processedAtMs: Date.now(),
    relativeTime: '0:00:01',
    searchText: 'bash',
    type: 'agent.tool_use',
    displayEvent: {
      id: 'evt_bash_123',
      type: 'tool_use',
      rawType: 'agent.tool_use',
      label: 'Bash',
      content: 'npm test',
      event: { id: 'evt_bash_123', type: 'agent.tool_use', name: 'Bash', input: { command: 'npm test' } },
      isQueued: false,
      isStreaming: false,
      isError: false,
      createdAtMs: Date.now(),
      processedAtMs: Date.now(),
      relativeTime: '0:00:01',
    },
    traceEntry: {
      id: 'evt_bash_123-0-tool_use',
      type: 'agent.tool_use',
      family: 'tool_use',
      label: 'Bash',
      preview: 'npm test',
      displayText: 'npm test',
      displayKind: 'command',
      event: { id: 'evt_bash_123', type: 'agent.tool_use', name: 'Bash', input: { command: 'npm test' } },
      createdAtMs: Date.now(),
      relativeTime: '0:00:01',
      rawEventId: 'evt_bash_123',
      searchText: 'bash npm test',
      isError: false,
    },
    event: {
      id: 'sevt_bash_public_123',
      type: 'agent.tool_use',
      name: 'Bash',
      input: { command: 'npm test' },
    },
    usage: { input: 0, output: 0, cacheRead: 0, cacheCreation: 0 },
  };

  test('detects tool types and public IDs correctly', () => {
    expect(isAskUserQuestionCall(baseToolCall)).toBe(false);

    const askCall: ToolCallEntry = {
      ...baseToolCall,
      name: 'Ask User Question',
      event: {
        id: 'sevt_ask_456',
        type: 'agent.custom_tool_use',
        name: 'AskUserQuestion',
      },
    };
    expect(isAskUserQuestionCall(askCall)).toBe(true);
    expect(sessionToolConfirmationPublicId(askCall)).toBe('sevt_ask_456');

    const fallbackCall: ToolCallEntry = {
      ...baseToolCall,
      event: { type: 'agent.tool_use' },
    };
    expect(sessionToolConfirmationPublicId(fallbackCall)).toBe('evt_bash_123');
  });

  test('renders tool approval card and calls onConfirm with allow and deny', async () => {
    const confirmations: unknown[] = [];
    const handleConfirm = async (input: unknown) => {
      confirmations.push(input);
    };

    const { rerender } = render(<SessionRequiresActionCard toolCall={baseToolCall} onConfirm={handleConfirm} />);

    expect(screen.getByTestId('session-tool-approval-card')).toBeTruthy();
    expect(screen.getByText('Bash')).toBeTruthy();
    expect(screen.getByText('npm test')).toBeTruthy();

    const allowBtn = screen.getByTestId('tool-allow-button');
    const denyBtn = screen.getByTestId('tool-deny-button');

    fireEvent.click(allowBtn);
    await waitFor(() => {
      expect(confirmations).toHaveLength(1);
      expect(confirmations[0]).toEqual({
        toolUseId: 'sevt_bash_public_123',
        result: 'allow',
        sessionThreadId: undefined,
      });
    });

    rerender(<SessionRequiresActionCard toolCall={baseToolCall} onConfirm={handleConfirm} />);
    fireEvent.click(denyBtn);
    await waitFor(() => {
      expect(confirmations).toHaveLength(2);
      expect(confirmations[1]).toEqual({
        toolUseId: 'sevt_bash_public_123',
        result: 'deny',
        sessionThreadId: undefined,
      });
    });
  });

  test('renders questionnaire with multiSelect and custom input, submitting questions as keys', async () => {
    const confirmations: any[] = [];
    const handleConfirm = async (input: unknown) => {
      confirmations.push(input);
    };

    const askToolCall: ToolCallEntry = {
      ...baseToolCall,
      name: 'Ask User Question',
      event: {
        id: 'sevt_ask_789',
        type: 'agent.custom_tool_use',
        name: 'AskUserQuestion',
        session_thread_id: 'sthr_subagent_1',
        input: {
          questions: [
            {
              header: 'Flavor',
              question: 'Which ice cream flavors do you want?',
              options: [
                { label: 'Vanilla', description: 'Classic vanilla bean' },
                { label: 'Chocolate', description: 'Dark cocoa' },
              ],
              multiSelect: true,
            },
            {
              header: 'Topping',
              question: 'Select a topping',
              options: [{ label: 'Sprinkles' }, { label: 'Nuts' }],
              multiSelect: false,
            },
          ],
        },
      },
    };

    render(<SessionRequiresActionCard toolCall={askToolCall} onConfirm={handleConfirm} />);

    expect(screen.getByTestId('session-questionnaire-card')).toBeTruthy();
    expect(screen.getByText('Which ice cream flavors do you want?')).toBeTruthy();
    expect(screen.getByText('Vanilla')).toBeTruthy();
    expect(screen.getByText('Classic vanilla bean')).toBeTruthy();

    // Select Vanilla and Chocolate for question 1
    fireEvent.click(screen.getByText('Vanilla'));
    fireEvent.click(screen.getByText('Chocolate'));

    // Click Next to advance to question 2
    const nextBtn = screen.getByRole('button', { name: 'Next' });
    expect(nextBtn).toBeTruthy();
    fireEvent.click(nextBtn);

    // Question 2 should now be visible
    expect(screen.getByText('Select a topping')).toBeTruthy();
    const sprinklesOption = screen.getByText('Sprinkles');
    fireEvent.click(sprinklesOption);

    // Submit the questionnaire
    const submitBtn = screen.getByRole('button', { name: 'Confirm' });
    fireEvent.click(submitBtn);

    await waitFor(() => {
      expect(confirmations).toHaveLength(1);
      const payload = confirmations[0];
      expect(payload.toolUseId).toBe('sevt_ask_789');
      expect(payload.result).toBe('allow');
      expect(payload.customTool).toBe(true);
      expect(payload.sessionThreadId).toBe('sthr_subagent_1');
      expect(payload.answers['Which ice cream flavors do you want?']).toEqual(['Vanilla', 'Chocolate']);
      expect(payload.answers['Select a topping']).toBe('Sprinkles');
    });
  });

  test('supports deny on questionnaire card', async () => {
    const confirmations: any[] = [];
    const handleConfirm = async (input: unknown) => {
      confirmations.push(input);
    };

    const askToolCall: ToolCallEntry = {
      ...baseToolCall,
      name: 'Ask User Question',
      event: {
        id: 'sevt_ask_deny',
        type: 'agent.custom_tool_use',
        name: 'AskUserQuestion',
        input: {
          questions: [
            {
              question: 'Do you agree?',
              options: [{ label: 'Yes' }, { label: 'No' }],
            },
          ],
        },
      },
    };

    render(<SessionRequiresActionCard toolCall={askToolCall} onConfirm={handleConfirm} />);

    const denyBtn = screen.getByTestId('questionnaire-deny-button');
    fireEvent.click(denyBtn);

    await waitFor(() => {
      expect(confirmations).toHaveLength(1);
      expect(confirmations[0]).toEqual({
        toolUseId: 'sevt_ask_deny',
        result: 'deny',
        customTool: true,
        sessionThreadId: undefined,
      });
    });
  });
});
