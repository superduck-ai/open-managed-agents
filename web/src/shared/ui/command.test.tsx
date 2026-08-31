import { afterEach, describe, expect, test } from 'bun:test';
import '../../test/setup';
import { Command, CommandInput } from './command';

const { cleanup, render, screen } = await import('@testing-library/react');

afterEach(cleanup);

describe('CommandInput', () => {
  test('marks the nested input and wrapper for the command-specific focus style', () => {
    render(
      <Command>
        <CommandInput aria-label="搜索" />
      </Command>,
    );

    const input = screen.getByRole('combobox');
    const wrapper = input.parentElement;

    expect(input.dataset.slot).toBe('command-input');
    expect(wrapper?.dataset.slot).toBe('command-input-wrapper');
    expect(wrapper?.className).toContain('has-[[data-slot=command-input]:focus-visible]:ring-2');
  });

  test('keeps command focus on its wrapper and scopes native search cancel suppression', async () => {
    const stylesheet = await Bun.file(new URL('../../styles/foundation.css', import.meta.url)).text();

    expect(stylesheet).toContain(":where([data-slot='input'], [data-slot='textarea'], [data-slot='command-input'])");
    expect(stylesheet).toContain("input[type='search'][data-custom-clear]::-webkit-search-cancel-button");
    expect(stylesheet).not.toContain("input[type='search']::-webkit-search-cancel-button");
    expect(stylesheet).toContain('-webkit-appearance: none');
  });
});
