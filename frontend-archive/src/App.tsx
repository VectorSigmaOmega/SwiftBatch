import React, { useEffect, useState } from "react";
import { Aurora } from "./designs/Aurora";
import { AuroraV1 } from "./designs/AuroraV1";
import { ConsoleLegacy } from "./designs/ConsoleLegacy";
import { ConsoleV1 } from "./designs/ConsoleV1";
import { Darkroom } from "./designs/Darkroom";
import { DarkroomV1 } from "./designs/DarkroomV1";
import { Manifold } from "./designs/Manifold";
import { ManifoldV1 } from "./designs/ManifoldV1";
import { Studio } from "./designs/Studio";
import { StudioV1 } from "./designs/StudioV1";

type DesignId =
  | "studio"
  | "aurora"
  | "darkroom"
  | "manifold"
  | "console-legacy"
  | "studio-v1"
  | "aurora-v1"
  | "darkroom-v1"
  | "console-v1"
  | "manifold-v1";

const ARCHIVE_DESIGNS: { id: DesignId; label: string; blurb: string }[] = [
  { id: "studio", label: "Studio", blurb: "Editorial calm" },
  { id: "aurora", label: "Aurora", blurb: "Warm gradient" },
  { id: "darkroom", label: "Darkroom", blurb: "Photographic" },
  { id: "manifold", label: "Manifold", blurb: "Maximal" },
  { id: "console-legacy", label: "Console Legacy", blurb: "Earlier live version" },
  { id: "studio-v1", label: "Studio v1", blurb: "Editorial spread · 100vh" },
  { id: "aurora-v1", label: "Aurora v1", blurb: "Halo · 100vh" },
  { id: "darkroom-v1", label: "Darkroom v1", blurb: "Plate · 100vh" },
  { id: "console-v1", label: "Console v1", blurb: "Workspace · 100vh" },
  { id: "manifold-v1", label: "Manifold v1", blurb: "Block maximal · 100vh" },
];

function readInitialDesign(): DesignId {
  if (typeof window === "undefined") return "console-v1";
  const params = new URLSearchParams(window.location.search);
  const requested = params.get("d") as DesignId | null;
  if (requested && ARCHIVE_DESIGNS.some((design) => design.id === requested)) return requested;
  const stored = window.localStorage.getItem("photon.archive.design") as DesignId | null;
  if (stored && ARCHIVE_DESIGNS.some((design) => design.id === stored)) return stored;
  return "console-v1";
}

function Footer() {
  return (
    <footer
      className="mt-6 flex flex-wrap items-center justify-between gap-3 pt-4 text-[11px]"
      style={{ borderTop: "1px solid currentColor", opacity: 0.5 }}
    >
      <span>Photon archive · Design sandbox</span>
      <span className="flex items-center gap-4">
        <a
          href="https://github.com/VectorSigmaOmega/Photon"
          target="_blank"
          rel="noreferrer"
          className="underline-offset-4 hover:underline"
        >
          GitHub
        </a>
        <a href="#" className="underline-offset-4 hover:underline">
          Portfolio
        </a>
      </span>
    </footer>
  );
}

function DesignSwitcher({
  active,
  onChange,
}: {
  active: DesignId;
  onChange: (design: DesignId) => void;
}) {
  return (
    <div className="fixed right-3 top-1/2 z-[60] flex -translate-y-1/2 items-center">
      <div
        className="flex flex-col gap-1 rounded-2xl p-1"
        style={{
          background: "rgba(10,10,10,0.92)",
          color: "#FAFAFA",
          border: "1px solid rgba(250,250,250,0.12)",
          backdropFilter: "blur(12px)",
          WebkitBackdropFilter: "blur(12px)",
          boxShadow: "0 6px 24px rgba(0,0,0,0.45)",
        }}
      >
        {ARCHIVE_DESIGNS.map((design, index) => {
          const isActive = design.id === active;
          return (
            <button
              key={design.id}
              type="button"
              onClick={() => onChange(design.id)}
              className="flex items-center gap-2 rounded-xl px-3 py-1.5 text-[12px] transition-all"
              style={{
                background: isActive ? "#FAFAFA" : "transparent",
                color: isActive ? "#0A0A0A" : "rgba(250,250,250,0.7)",
                fontWeight: isActive ? 600 : 500,
              }}
              title={`${design.label} — ${design.blurb} (press ${index + 1})`}
            >
              <span className="w-3 text-[10px] tabular-nums" style={{ opacity: 0.55 }}>
                {index + 1}
              </span>
              <span>{design.label}</span>
            </button>
          );
        })}
      </div>
    </div>
  );
}

export default function App() {
  const [activeDesign, setActiveDesign] = useState<DesignId>(readInitialDesign);
  const fullFooter = <Footer />;
  const slimFooter = <Footer />;

  useEffect(() => {
    window.localStorage.setItem("photon.archive.design", activeDesign);
    const url = new URL(window.location.href);
    url.searchParams.set("d", activeDesign);
    window.history.replaceState(null, "", url.toString());
  }, [activeDesign]);

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      const target = event.target as HTMLElement | null;
      if (
        target &&
        (target.tagName === "INPUT" ||
          target.tagName === "TEXTAREA" ||
          target.isContentEditable)
      ) {
        return;
      }
      const index = parseInt(event.key, 10);
      if (!Number.isNaN(index) && index >= 1 && index <= ARCHIVE_DESIGNS.length) {
        setActiveDesign(ARCHIVE_DESIGNS[index - 1].id);
      }
    };

    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  let body: React.ReactNode;
  switch (activeDesign) {
    case "studio":
      body = <Studio footer={fullFooter} />;
      break;
    case "aurora":
      body = <Aurora footer={fullFooter} />;
      break;
    case "darkroom":
      body = <Darkroom footer={fullFooter} />;
      break;
    case "manifold":
      body = <Manifold footer={fullFooter} />;
      break;
    case "console-legacy":
      body = <ConsoleLegacy footer={fullFooter} />;
      break;
    case "studio-v1":
      body = <StudioV1 footer={slimFooter} />;
      break;
    case "aurora-v1":
      body = <AuroraV1 footer={slimFooter} />;
      break;
    case "darkroom-v1":
      body = <DarkroomV1 footer={slimFooter} />;
      break;
    case "console-v1":
      body = <ConsoleV1 footer={slimFooter} />;
      break;
    case "manifold-v1":
      body = <ManifoldV1 footer={slimFooter} />;
      break;
  }

  return (
    <div className="relative">
      {body}
      <DesignSwitcher active={activeDesign} onChange={setActiveDesign} />
    </div>
  );
}
