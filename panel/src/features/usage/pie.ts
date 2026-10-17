export const PERSON_COLORS = [
  '#5386c6',
  '#a47dc4',
  '#c58b45',
  '#5e9b86',
  '#c76e7b',
  '#6a9cae',
  '#948b56',
];
export function personColor(id: string) {
  let hash = 0;
  for (const c of id) hash = (hash * 31 + c.charCodeAt(0)) >>> 0;
  return PERSON_COLORS[hash % PERSON_COLORS.length];
}
export function donutSegments<T extends { value: number }>(parts: readonly T[]) {
  return parts
    .filter((part) => part.value > 0)
    .reduce<(T & { start: number; gap: number; middle: number })[]>((segments, part) => {
      const previous = segments[segments.length - 1];
      const start = previous ? previous.start + previous.value : 0;
      const gap = part.value >= 99.999999 ? 0 : Math.min(0.8, part.value * 0.12);
      return [...segments, { ...part, start, gap, middle: (start + part.value / 2) * 3.6 - 90 }];
    }, []);
}

export function donutPath(start: number, value: number, inner = 30, outer = 44): string {
  if (value >= 99.999999)
    return `M50 ${50 - outer} A${outer} ${outer} 0 1 1 50 ${50 + outer} A${outer} ${outer} 0 1 1 50 ${50 - outer} M50 ${50 - inner} A${inner} ${inner} 0 1 0 50 ${50 + inner} A${inner} ${inner} 0 1 0 50 ${50 - inner}`;
  const point = (percent: number, radius: number) => {
    const angle = ((percent * 3.6 - 90) * Math.PI) / 180;
    return `${50 + radius * Math.cos(angle)} ${50 + radius * Math.sin(angle)}`;
  };
  const large = value > 50 ? 1 : 0;
  return `M${point(start, outer)} A${outer} ${outer} 0 ${large} 1 ${point(start + value, outer)} L${point(start + value, inner)} A${inner} ${inner} 0 ${large} 0 ${point(start, inner)} Z`;
}

export function piePath(start: number, value: number): string {
  if (value >= 99.999999) return 'M50 0 A50 50 0 1 1 50 100 A50 50 0 1 1 50 0 Z';
  const point = (percent: number) => {
    const angle = ((percent * 3.6 - 90) * Math.PI) / 180;
    return [50 + 50 * Math.cos(angle), 50 + 50 * Math.sin(angle)];
  };
  const a = point(start),
    b = point(start + value);
  return `M50 50 L${a[0]} ${a[1]} A50 50 0 ${value > 50 ? 1 : 0} 1 ${b[0]} ${b[1]} Z`;
}
