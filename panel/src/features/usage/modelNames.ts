const ALIASES: Record<string, string> = {
  'qwen-coder': 'Qwen 3.8 27B',
  'qwen-vl': 'Qwen 3 VL 30B A3B',
  'nemotron-120b': 'Nemotron 3 Super 120B',
  'nemotron-3.5-lightning': 'Nemotron 3.5 Lightning',
  'gpt-oss-120b': 'GPT OSS 120B',
  'gemma-4-31b': 'Gemma 4 31B',
  'muse-glimmer-30b': 'Muse Glimmer 30B',
};
const WORDS: Record<string, string> = {
  gpt: 'GPT',
  glm: 'GLM',
  oss: 'OSS',
  vl: 'VL',
  tts: 'TTS',
  ai: 'AI',
  qwen: 'Qwen',
  minimax: 'MiniMax',
  zai: 'Z.ai',
};

const MODES: Record<string, string> = { fast: 'Fast', ultrafast: 'Ultrafast' };

/** Usage ids may carry a speed mode suffix (`model@ultrafast`). */
export function friendlyModelName(id: string, displayName?: string): string {
  const mode = /@([^@/]+)$/.exec(id)?.[1];
  if (mode) {
    const base = friendlyModelName(id.slice(0, -(mode.length + 1)), displayName);
    return `${base} (${MODES[mode] ?? mode})`;
  }
  const key = id
    .replace(/^spark\//, '')
    .replace(/(claude-(?:opus|sonnet|haiku|mythos|fable)-)(\d+)-(\d+)(?=$|-)/i, '$1$2.$3');
  if (ALIASES[key]) return ALIASES[key];
  const raw = /\.gguf$/i.test(key)
    ? (key.split('/').pop() ?? key)
        .replace(/\.gguf$/i, '')
        .replace(/-(?:UD-)?(?:Q\d.*|F16|BF16|NVFP4)$/i, '')
    : key;
  const text = displayName && displayName !== id ? displayName : raw;
  return text
    .replace(/[-_:]+/g, ' ')
    .split(/\s+/)
    .map(
      (word) =>
        WORDS[word.toLowerCase()] ??
        (/^a?\d+[ba]$/i.test(word)
          ? word.toUpperCase()
          : word.charAt(0).toUpperCase() + word.slice(1))
    )
    .join(' ');
}
