import type { Point, Zone } from "@/api/types";

// Зоны хранятся нормализованно [0..1] относительно полного повёрнутого кадра (docs/07-api.md).
// Холст редактора — width×height пикселей до масштабирования сцены.

export interface Size {
  width: number;
  height: number;
}

const clamp01 = (v: number) => Math.min(1, Math.max(0, v));

export function toPx([u, v]: Point, s: Size): Point {
  return [u * s.width, v * s.height];
}

export function toNorm([x, y]: Point, s: Size): Point {
  return [round(clamp01(x / s.width)), round(clamp01(y / s.height))];
}

/** 4 знака после запятой — 0.1 px на 1920; меньше шума в JSON и истории версий. */
function round(v: number) {
  return Math.round(v * 10000) / 10000;
}

export function flat(points: Point[], s: Size): number[] {
  return points.flatMap((p) => toPx(p, s));
}

export function dist(a: Point, b: Point) {
  return Math.hypot(a[0] - b[0], a[1] - b[1]);
}

export function midpoint(a: Point, b: Point): Point {
  return [round((a[0] + b[0]) / 2), round((a[1] + b[1]) / 2)];
}

/** Сдвинуть полигон на (dx, dy) в нормализованных координатах, не выходя за кадр. */
export function translate(points: Point[], dx: number, dy: number): Point[] {
  const xs = points.map((p) => p[0]);
  const ys = points.map((p) => p[1]);
  const cdx = Math.min(Math.max(dx, -Math.min(...xs)), 1 - Math.max(...xs));
  const cdy = Math.min(Math.max(dy, -Math.min(...ys)), 1 - Math.max(...ys));
  return points.map(([x, y]) => [round(x + cdx), round(y + cdy)]);
}

export function area(points: Point[]) {
  let a = 0;
  for (let i = 0; i < points.length; i++) {
    const [x1, y1] = points[i];
    const [x2, y2] = points[(i + 1) % points.length];
    a += x1 * y2 - x2 * y1;
  }
  return Math.abs(a) / 2;
}

/** Сумма модулей площадей треугольников веером от первой точки: 0 только если все точки на одной прямой. */
export function spread(points: Point[]) {
  const [ox, oy] = points[0];
  let a = 0;
  for (let i = 1; i < points.length - 1; i++) {
    const [x1, y1] = points[i];
    const [x2, y2] = points[i + 1];
    a += Math.abs((x1 - ox) * (y2 - oy) - (x2 - ox) * (y1 - oy));
  }
  return a / 2;
}

function segmentsCross(p1: Point, p2: Point, p3: Point, p4: Point) {
  const d = (a: Point, b: Point, c: Point) => (b[0] - a[0]) * (c[1] - a[1]) - (b[1] - a[1]) * (c[0] - a[0]);
  const d1 = d(p3, p4, p1);
  const d2 = d(p3, p4, p2);
  const d3 = d(p1, p2, p3);
  const d4 = d(p1, p2, p4);
  return d1 * d2 < 0 && d3 * d4 < 0;
}

/** Есть ли пересечение несмежных рёбер полигона. */
export function selfIntersects(points: Point[]) {
  const n = points.length;
  for (let i = 0; i < n; i++) {
    for (let j = i + 1; j < n; j++) {
      if (Math.abs(i - j) <= 1 || (i === 0 && j === n - 1)) continue;
      if (segmentsCross(points[i], points[(i + 1) % n], points[j], points[(j + 1) % n])) return true;
    }
  }
  return false;
}

export interface ZoneCheck {
  errors: string[];
  warnings: string[];
}

/** Проверки перед сохранением (docs/06-panel.md §5): как на сервере + предупреждение о самопересечении. */
export function checkZones(zones: Zone[]): ZoneCheck {
  const errors: string[] = [];
  const warnings: string[] = [];
  if (!zones.some((z) => z.type === "MONITOR")) errors.push("Нужна хотя бы одна зона MONITOR");
  zones.forEach((z, i) => {
    const name = `Зона ${i + 1}`;
    if (z.points.length < 3) errors.push(`${name}: нужно не меньше 3 точек`);
    else if (spread(z.points) < 1e-6) errors.push(`${name}: нулевая площадь`);
    else if (selfIntersects(z.points)) warnings.push(`${name}: стороны пересекаются`);
  });
  return { errors, warnings };
}

/**
 * Точка вставки на ребре: ближайшее к курсору ребро и индекс, куда вставить новую вершину.
 */
export function nearestEdge(points: Point[], p: Point): { index: number; distance: number } {
  let best = { index: 1, distance: Infinity };
  for (let i = 0; i < points.length; i++) {
    const a = points[i];
    const b = points[(i + 1) % points.length];
    const l2 = (b[0] - a[0]) ** 2 + (b[1] - a[1]) ** 2;
    const t = l2 === 0 ? 0 : Math.max(0, Math.min(1, ((p[0] - a[0]) * (b[0] - a[0]) + (p[1] - a[1]) * (b[1] - a[1])) / l2));
    const d = dist(p, [a[0] + t * (b[0] - a[0]), a[1] + t * (b[1] - a[1])]);
    if (d < best.distance) best = { index: i + 1, distance: d };
  }
  return best;
}

/** Прямоугольник выреза [x0,y0,x1,y1] → перевод зон в координаты выреза (для наложения на кадр change). */
export function toCrop([u, v]: Point, crop: [number, number, number, number]): Point {
  const [x0, y0, x1, y1] = crop;
  return [(u - x0) / (x1 - x0), (v - y0) / (y1 - y0)];
}
