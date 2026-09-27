import { describe, expect, it } from "vitest";
import type { Point, Zone } from "@/api/types";
import { checkZones, nearestEdge, selfIntersects, toCrop, toNorm, toPx, translate } from "./geometry";

const size = { width: 800, height: 450 };

describe("координаты холста", () => {
  it("toPx и toNorm взаимно обратны", () => {
    const p: Point = [0.25, 0.6];
    expect(toNorm(toPx(p, size), size)).toEqual(p);
  });

  it("точки за пределами кадра прижимаются к краю", () => {
    expect(toNorm([-10, 500], size)).toEqual([0, 1]);
  });

  it("сдвиг полигона не выводит его за кадр", () => {
    const pts: Point[] = [[0.8, 0.1], [0.95, 0.1], [0.95, 0.3]];
    const moved = translate(pts, 0.2, -0.5);
    expect(Math.max(...moved.map((p) => p[0]))).toBeCloseTo(1);
    expect(Math.min(...moved.map((p) => p[1]))).toBeCloseTo(0);
    expect(moved[1][0] - moved[0][0]).toBeCloseTo(0.15); // форма сохранилась
  });

  it("зона в координатах выреза", () => {
    expect(toCrop([0.5, 0.5], [0.25, 0.25, 0.75, 0.75])).toEqual([0.5, 0.5]);
    expect(toCrop([0.25, 0.75], [0.25, 0.25, 0.75, 0.75])).toEqual([0, 1]);
  });
});

describe("проверки зон", () => {
  const square: Point[] = [[0.1, 0.1], [0.5, 0.1], [0.5, 0.5], [0.1, 0.5]];
  const bowtie: Point[] = [[0.1, 0.1], [0.5, 0.5], [0.5, 0.1], [0.1, 0.5]];

  it("нужен MONITOR", () => {
    const zs: Zone[] = [{ type: "IGNORE", points: square }];
    expect(checkZones(zs).errors).toContain("Нужна хотя бы одна зона MONITOR");
  });

  it("меньше трёх точек и нулевая площадь — ошибка", () => {
    const zs: Zone[] = [
      { type: "MONITOR", points: square },
      { type: "IGNORE", points: [[0, 0], [1, 1]] },
      { type: "IGNORE", points: [[0, 0], [0.5, 0.5], [1, 1]] },
    ];
    expect(checkZones(zs).errors).toHaveLength(2);
  });

  it("самопересечение — предупреждение, не ошибка", () => {
    expect(selfIntersects(bowtie)).toBe(true);
    expect(selfIntersects(square)).toBe(false);
    const r = checkZones([{ type: "MONITOR", points: bowtie }]);
    expect(r.errors).toHaveLength(0);
    expect(r.warnings).toHaveLength(1);
  });

  it("ближайшее ребро для вставки точки", () => {
    expect(nearestEdge(square, [0.3, 0.12]).index).toBe(1); // верхнее ребро 0→1
    expect(nearestEdge(square, [0.09, 0.3]).index).toBe(4); // левое ребро 3→0
  });
});
