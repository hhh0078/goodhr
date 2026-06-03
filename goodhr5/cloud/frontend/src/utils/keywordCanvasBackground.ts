// 本文件负责渲染 GoodHR 登录页和官网共用的 OGL 三层关键词 3D 背景。

import { Geometry, Mesh, Program, Renderer, Texture } from "ogl";

export type KeywordCanvasBackground = {
  destroy: () => void;
};

type KeywordCanvasOptions = {
  rows?: string[][];
  rowCount?: number;
  speed?: number;
  minFontSize?: number;
  maxFontSize?: number;
  fontScale?: number;
  opacity?: number;
};

type KeywordLayer = {
  mesh: Mesh;
  texture: Texture;
  program: Program;
  speed: number;
};

const DEFAULT_ROWS = [
  ["招聘", "候选人", "简历", "打招呼", "沟通", "面试", "筛选", "匹配"],
  ["Boss直聘", "猎聘", "智联", "58同城", "HR", "岗位模板", "AI评分"],
  ["自动筛选", "自动打招呼", "人才库", "回复率", "复聊", "跟进", "Offer"],
  ["薪资", "经验", "学历", "城市", "活跃候选人", "高匹配", "已沟通"],
  ["今日打招呼", "跳过原因", "查看详情", "推荐列表", "招聘效率", "沟通记录"],
  ["AI判断", "匹配分", "已扫描", "已跳过", "待跟进", "高意向"],
  ["成都招聘", "销售", "客服", "运营", "老师", "开发", "人事"],
  ["自动化", "批量沟通", "精准筛选", "快速开聊", "职位匹配", "人才发现"],
];

const VERTEX_SHADER = `
attribute vec2 position;
attribute vec2 uv;

uniform float uSkew;
uniform float uDepthScale;

varying vec2 vUv;

void main() {
  vec2 nextPosition = position * uDepthScale;
  nextPosition.x += nextPosition.y * uSkew;
  vUv = uv;
  gl_Position = vec4(nextPosition, 0.0, 1.0);
}
`;

const FRAGMENT_SHADER = `
precision mediump float;

uniform sampler2D tMap;
uniform float uTime;
uniform float uSpeed;
uniform float uOpacity;
uniform float uDirection;

varying vec2 vUv;

void main() {
  vec2 nextUv = vUv;
  nextUv.x = fract(nextUv.x + uTime * uSpeed * uDirection);
  vec4 color = texture2D(tMap, nextUv);
  gl_FragColor = vec4(color.rgb, color.a * uOpacity);
}
`;

/**
 * 创建关键词动态背景。
 *
 * @param host - 承载 canvas 的 HTML 元素。
 * @param options - 背景密度、速度和字号配置。
 * @returns 背景销毁句柄。
 */
export async function createKeywordCanvasBackground(
  host: HTMLElement,
  options: KeywordCanvasOptions = {},
): Promise<KeywordCanvasBackground | null> {
  if (!host) return null;

  const config = normalizeOptions(options);
  const renderer = new Renderer({
    alpha: true,
    antialias: true,
    dpr: Math.min(window.devicePixelRatio || 1, 1.5),
    powerPreference: "high-performance",
  });
  const gl = renderer.gl;
  gl.canvas.className = "keyword-canvas";
  gl.clearColor(0, 0, 0, 0);
  gl.enable(gl.BLEND);
  gl.blendFunc(gl.SRC_ALPHA, gl.ONE_MINUS_SRC_ALPHA);
  host.appendChild(gl.canvas);

  const geometry = createFullscreenGeometry(gl);
  const layers = createKeywordLayers(gl, geometry, config);
  let disposed = false;
  let frameID = 0;

  const resize = () => {
    renderer.setSize(host.clientWidth || window.innerWidth, host.clientHeight || window.innerHeight);
  };
  const animate = (time: number) => {
    if (disposed) return;
    frameID = window.requestAnimationFrame(animate);
    gl.clear(gl.COLOR_BUFFER_BIT);
    const seconds = time * 0.001;
    for (const layer of layers) {
      layer.program.uniforms.uTime.value = seconds;
      renderer.render({ scene: layer.mesh, clear: false });
    }
  };

  resize();
  window.addEventListener("resize", resize);
  frameID = window.requestAnimationFrame(animate);

  return {
    destroy: () => {
      disposed = true;
      window.cancelAnimationFrame(frameID);
      window.removeEventListener("resize", resize);
      for (const layer of layers) {
        layer.texture.image = null;
      }
      gl.canvas.remove();
    },
  };
}

/**
 * 初始化页面中声明式配置的关键词背景。
 *
 * @param selector - 需要初始化的背景容器选择器。
 * @param options - 背景配置。
 */
export function mountKeywordCanvasBackgrounds(selector = "[data-keyword-canvas]", options: KeywordCanvasOptions = {}) {
  document.querySelectorAll<HTMLElement>(selector).forEach((host) => {
    createKeywordCanvasBackground(host, options);
  });
}

/**
 * 合并关键词背景默认配置。
 *
 * @param options - 外部传入配置。
 * @returns 完整配置。
 */
function normalizeOptions(options: KeywordCanvasOptions) {
  return {
    rows: options.rows?.length ? options.rows : DEFAULT_ROWS,
    rowCount: options.rowCount || 16,
    speed: options.speed || 1.18,
    minFontSize: options.minFontSize || 42,
    maxFontSize: options.maxFontSize || 98,
    fontScale: options.fontScale || 0.078,
    opacity: options.opacity || 1,
  };
}

/**
 * 创建覆盖全屏的 WebGL 几何体。
 *
 * @param gl - WebGL 上下文。
 * @returns OGL 几何体。
 */
function createFullscreenGeometry(gl: WebGLRenderingContext) {
  return new Geometry(gl, {
    position: {
      size: 2,
      data: new Float32Array([-1, -1, 3, -1, -1, 3]),
    },
    uv: {
      size: 2,
      data: new Float32Array([0, 0, 5, 0, 0, 3]),
    },
  });
}

/**
 * 创建三层关键词纹理平面。
 *
 * @param gl - WebGL 上下文。
 * @param geometry - 共享几何体。
 * @param config - 背景完整配置。
 * @returns 关键词平面列表。
 */
function createKeywordLayers(
  gl: WebGLRenderingContext,
  geometry: Geometry,
  config: ReturnType<typeof normalizeOptions>,
) {
  const layerConfigs = [
    { scale: 1.2, fontSize: Math.min(config.maxFontSize, 74), color: "rgba(36, 255, 84, 0.38)", opacity: 0.82, speed: 0.018, direction: -1, skew: -0.18 },
    { scale: 1.08, fontSize: Math.max(config.minFontSize, Math.min(config.maxFontSize * 0.62, 52)), color: "rgba(24, 150, 50, 0.3)", opacity: 0.66, speed: 0.012, direction: 1, skew: -0.12 },
    { scale: 1.0, fontSize: Math.max(config.minFontSize * 0.72, Math.min(config.maxFontSize * 0.38, 34)), color: "rgba(12, 86, 28, 0.26)", opacity: 0.56, speed: 0.007, direction: -1, skew: -0.08 },
  ];

  return layerConfigs.map((layerConfig, index) => {
    const texture = new Texture(gl, {
      image: createKeywordTexture(config.rows, config.rowCount, layerConfig.fontSize, layerConfig.color, index),
      generateMipmaps: false,
      wrapS: gl.REPEAT,
      wrapT: gl.REPEAT,
    });
    texture.needsUpdate = true;
    const program = new Program(gl, {
      vertex: VERTEX_SHADER,
      fragment: FRAGMENT_SHADER,
      transparent: true,
      uniforms: {
        tMap: { value: texture },
        uTime: { value: 0 },
        uSpeed: { value: layerConfig.speed * config.speed },
        uOpacity: { value: layerConfig.opacity * config.opacity },
        uDirection: { value: layerConfig.direction },
        uSkew: { value: layerConfig.skew },
        uDepthScale: { value: layerConfig.scale },
      },
    });
    return {
      mesh: new Mesh(gl, { geometry, program }),
      texture,
      program,
      speed: layerConfig.speed,
    };
  });
}

/**
 * 将关键词绘制成可重复采样的 Canvas 纹理。
 *
 * @param rows - 关键词行数据。
 * @param rowCount - 行数。
 * @param fontSize - 字号。
 * @param color - 文本颜色。
 * @param layerIndex - 当前层级。
 * @returns Canvas 纹理。
 */
function createKeywordTexture(rows: string[][], rowCount: number, fontSize: number, color: string, layerIndex: number) {
  const canvas = document.createElement("canvas");
  canvas.width = 2048;
  canvas.height = 1024;
  const ctx = canvas.getContext("2d");
  if (!ctx) return canvas;

  ctx.clearRect(0, 0, canvas.width, canvas.height);
  ctx.font = `700 ${fontSize}px Arial, Helvetica, sans-serif`;
  ctx.textBaseline = "middle";
  ctx.fillStyle = color;

  const lineHeight = canvas.height / Math.max(8, rowCount);
  for (let index = 0; index < rowCount + 2; index += 1) {
    const row = rows[(index + layerIndex) % rows.length];
    const text = Array.from({ length: 7 }, () => row.join("   ")).join("   ");
    const x = (index % 2 === 0 ? -120 : -520) - layerIndex * 90;
    const y = (index + 0.35) * lineHeight;
    ctx.fillText(text, x, y);
  }
  return canvas;
}
