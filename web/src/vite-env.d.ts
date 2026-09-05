/// <reference types="vite/client" />

declare module "@novnc/novnc" {
  export default class RFB {
    constructor(target: HTMLElement, url: string, options?: object);
    disconnect(): void;
    scaleViewport: boolean;
    clipViewport: boolean;
    background: string;
    addEventListener(type: string, fn: (e: Event) => void): void;
  }
}
