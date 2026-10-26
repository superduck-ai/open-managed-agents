import { type I18nMsg } from '../types';
import { isBudgetReachedError } from './budget';

export function budgetErrorMessage(message: string | null, msg: I18nMsg, reached = false): string | null {
  if (!message) return message;
  if (isBudgetReachedError(message)) {
    return reached
      ? null
      : msg(
          'managedAgents.budget.rejected',
          'The session budget has been reached. Change or remove the budget to continue.',
        );
  }
  const unpricedPrefix = 'budget requires models with a list price; no list price configured for: ';
  if (message.startsWith(unpricedPrefix)) {
    return msg(
      'managedAgents.budget.unpriced',
      'Configure list prices for these models before setting a budget: {models}',
      { models: message.slice(unpricedPrefix.length) },
    );
  }
  if (message === "budget.max_list_cost must be greater than the session's consumed list cost") {
    return msg('managedAgents.budget.aboveSpent', 'The new budget must be greater than the amount already spent.');
  }
  if (
    message === 'budgets can only be attached when creating a session' ||
    message === 'a removed budget cannot be added again'
  ) {
    return msg(
      'managedAgents.budget.createOnly',
      'A budget can only be set when creating a session. Once removed, it cannot be added again.',
    );
  }
  if (message === 'session has no budget to remove') {
    return msg('managedAgents.budget.noBudget', 'This session has no budget to remove.');
  }
  if (
    message.startsWith('budget.max_list_cost.') ||
    message.startsWith('budget.type ') ||
    message === 'budget must be an object'
  ) {
    return msg('managedAgents.budget.invalid', 'Enter a positive dollar amount, e.g. 25 or 12.50.');
  }
  return message;
}
