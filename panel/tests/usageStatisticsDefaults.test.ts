import { describe, expect, test } from 'bun:test';
import { parseDocument } from 'yaml';
import { DEFAULT_VISUAL_VALUES } from '../src/types/visualConfig';
import { runVisualConfig } from './helpers/visualConfig';

describe('default-on usage collection', () => {
  test('new and omitted v8 settings agree with the backend default', () => {
    expect(DEFAULT_VISUAL_VALUES.usageStatisticsEnabled).toBe(true);
    for (const source of ['server: {port: 8317}\n', 'observability: {usage: {}}\n']) {
      expect(runVisualConfig(source).visualValues.usageStatisticsEnabled).toBe(true);
    }
  });
  test('an explicit opt-out is preserved through unrelated edits', () => {
    const source = 'observability:\n  usage:\n    usage-statistics-enabled: false\n';
    const config = runVisualConfig(source, [{ debug: true }]);
    expect(config.visualValues.usageStatisticsEnabled).toBe(false);
    const saved = config.applyVisualChangesToYaml(source);
    expect(parseDocument(saved).getIn(['observability', 'usage', 'usage-statistics-enabled'])).toBe(
      false
    );
  });
  test('disabling the new default writes an explicit false', () => {
    const source = 'server: {port: 8317}\n';
    const config = runVisualConfig(source, [{ usageStatisticsEnabled: false }]);
    const saved = config.applyVisualChangesToYaml(source);
    expect(parseDocument(saved).getIn(['observability', 'usage', 'usage-statistics-enabled'])).toBe(
      false
    );
    expect(runVisualConfig(saved).visualValues.usageStatisticsEnabled).toBe(false);
  });
});
