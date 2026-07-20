// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import type { DiagramRenderData } from "@layerdraw/render";
import { createElement, useEffect, useRef, useState, type KeyboardEvent, type ReactNode } from "react";
import * as THREE from "three";
import { OrbitControls } from "three/addons/controls/OrbitControls.js";

export interface DesktopThreeViewerProps {
  readonly data: DiagramRenderData;
  readonly selected: ReadonlySet<string>;
  readonly onSelectionChange: (keys: readonly string[]) => void;
}

function labelFor(data: DiagramRenderData, key: string): string {
  return data.labels.find((label) => label.anchor.kind === "occurrence" && label.anchor.occurrence_key === key)?.text ?? key;
}

function layerIndex(data: DiagramRenderData, key: string): number {
  const index = data.containers.findIndex((container) => container.child_keys.includes(key));
  return index < 0 ? 0 : index;
}

/**
 * The Desktop 2.5D surface consumes the same owner-produced RenderData as 2D.
 * Three.js owns only projection and interaction; it does not infer semantics.
 */
export function DesktopThreeViewer({ data, selected, onSelectionChange }: DesktopThreeViewerProps): ReactNode {
  const host = useRef<HTMLDivElement>(null);
  const [failure, setFailure] = useState(false);

  useEffect(() => {
    const element = host.current;
    if (element === null) return;
    setFailure(false);
    let renderer: THREE.WebGLRenderer;
    try {
      renderer = new THREE.WebGLRenderer({ antialias: true, alpha: false, powerPreference: "high-performance" });
    } catch {
      setFailure(true);
      return;
    }
    renderer.setClearColor(0xf7f8f5, 1);
    renderer.setPixelRatio(Math.min(window.devicePixelRatio, 2));
    renderer.domElement.className = "ld-desktop-viewer-three-canvas";
    renderer.domElement.setAttribute("role", "img");
    renderer.domElement.setAttribute("aria-label", "Diagram 2.5D view. Drag to rotate and scroll to zoom.");
    renderer.domElement.dataset.renderBackend = "three.js";
    element.prepend(renderer.domElement);

    const scene = new THREE.Scene();
    const camera = new THREE.PerspectiveCamera(42, 1, .1, 100);
    camera.position.set(0, 1.4, 8);
    const controls = new OrbitControls(camera, renderer.domElement);
    controls.enableDamping = false;
    controls.enablePan = true;
    controls.minDistance = 3;
    controls.maxDistance = 18;
    controls.target.set(0, 0, 0);

    scene.add(new THREE.HemisphereLight(0xffffff, 0x49645c, 2.2));
    const width = Math.max(data.bounds.width, 1);
    const height = Math.max(data.bounds.height, 1);
    const scale = 5 / Math.max(width, height);
    const toX = (x: number): number => (x - data.bounds.x - width / 2) * scale;
    const toY = (y: number): number => -(y - data.bounds.y - height / 2) * scale;

    const layers = data.containers.length > 0 ? data.containers : [{ render_key: "layer:default", bounds: data.bounds, child_keys: data.occurrences.map((item) => item.render_key) }];
    layers.forEach((layer, index) => {
      const geometry = new THREE.PlaneGeometry(Math.max(layer.bounds.width * scale, .5), Math.max(layer.bounds.height * scale, .5));
      const material = new THREE.MeshStandardMaterial({ color: index % 2 === 0 ? 0xdcefe9 : 0xe8eef5, transparent: true, opacity: .42, side: THREE.DoubleSide, depthWrite: false });
      const mesh = new THREE.Mesh(geometry, material);
      mesh.position.set(toX(layer.bounds.x + layer.bounds.width / 2), toY(layer.bounds.y + layer.bounds.height / 2), index * -.75);
      scene.add(mesh);
      const edges = new THREE.EdgesGeometry(geometry);
      const outline = new THREE.LineSegments(edges, new THREE.LineBasicMaterial({ color: 0x49766b }));
      outline.position.copy(mesh.position);
      scene.add(outline);
    });

    data.occurrences.forEach((item) => {
      const depth = layerIndex(data, item.render_key) * -.75 + .08;
      const geometry = new THREE.PlaneGeometry(Math.max(item.bounds.width * scale, .25), Math.max(item.bounds.height * scale, .15));
      const material = new THREE.MeshStandardMaterial({ color: selected.has(item.render_key) ? 0x72d5bd : 0xffffff, roughness: .72, side: THREE.DoubleSide });
      const mesh = new THREE.Mesh(geometry, material);
      mesh.position.set(toX(item.bounds.x + item.bounds.width / 2), toY(item.bounds.y + item.bounds.height / 2), depth);
      scene.add(mesh);
      const outline = new THREE.LineSegments(new THREE.EdgesGeometry(geometry), new THREE.LineBasicMaterial({ color: selected.has(item.render_key) ? 0x0f6b58 : 0x59746c }));
      outline.position.copy(mesh.position);
      scene.add(outline);
    });

    const ports = new Map(data.ports.map((port) => [port.render_key, port]));
    data.edge_paths.forEach((edge) => {
      const from = ports.get(edge.from_port_key);
      const to = ports.get(edge.to_port_key);
      const fromDepth = from === undefined ? .14 : layerIndex(data, from.occurrence_key) * -.75 + .14;
      const toDepth = to === undefined ? fromDepth : layerIndex(data, to.occurrence_key) * -.75 + .14;
      const denominator = Math.max(edge.points.length - 1, 1);
      const points = edge.points.map((point, index) => new THREE.Vector3(toX(point.x), toY(point.y), fromDepth + (toDepth - fromDepth) * index / denominator));
      const geometry = new THREE.BufferGeometry().setFromPoints(points);
      const relation = new THREE.Line(geometry, new THREE.LineBasicMaterial({ color: 0x0f6b58 }));
      relation.name = edge.render_key;
      scene.add(relation);
    });

    const resize = (): void => {
      const bounds = element.getBoundingClientRect();
      const nextWidth = Math.max(Math.floor(bounds.width), 1);
      const nextHeight = Math.max(Math.floor(bounds.height), 320);
      renderer.setSize(nextWidth, nextHeight, false);
      camera.aspect = nextWidth / nextHeight;
      camera.updateProjectionMatrix();
      renderer.render(scene, camera);
      renderer.domElement.dataset.renderReady = "true";
      renderer.domElement.dataset.camera = camera.position.toArray().map((value) => value.toFixed(4)).join(",");
      renderer.domElement.dataset.relationCount = String(data.edge_paths.length);
      renderer.domElement.dataset.crossLayerRelationCount = String(data.edge_paths.filter((edge) => {
        const from = ports.get(edge.from_port_key);
        const to = ports.get(edge.to_port_key);
        return from !== undefined && to !== undefined && layerIndex(data, from.occurrence_key) !== layerIndex(data, to.occurrence_key);
      }).length);
    };
    const observer = new ResizeObserver(resize);
    observer.observe(element);
    controls.addEventListener("change", resize);
    resize();
    return () => {
      observer.disconnect();
      controls.removeEventListener("change", resize);
      controls.dispose();
      scene.traverse((object) => {
        if (object instanceof THREE.Mesh || object instanceof THREE.Line) {
          object.geometry.dispose();
          const materials = Array.isArray(object.material) ? object.material : [object.material];
          materials.forEach((material) => material.dispose());
        }
      });
      renderer.dispose();
      renderer.domElement.remove();
    };
  }, [data, selected]);

  if (failure) return createElement("p", { role: "alert", className: "ld-desktop-viewer-status" }, "The 2.5D renderer is unavailable.");
  return createElement("div", { ref: host, className: "ld-desktop-viewer-three", "data-view-mode": "2.5d" },
    createElement("ul", { className: "ld-desktop-visually-hidden", "aria-label": "2.5D diagram items" }, data.occurrences.map((item) =>
      createElement("li", { key: item.render_key }, createElement("button", {
        type: "button", "aria-pressed": selected.has(item.render_key), onClick: () => onSelectionChange([item.render_key]),
        onKeyDown: (event: KeyboardEvent) => {
          if (event.key !== "Enter" && event.key !== " ") return;
          event.preventDefault();
          onSelectionChange([item.render_key]);
        },
      }, labelFor(data, item.render_key))))));
}
