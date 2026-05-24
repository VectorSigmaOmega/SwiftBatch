import { useCallback, useEffect, useId, useMemo, useRef, useState } from "react";
import { SiteFooter } from "../components/SiteFooter";
import { formatBytes } from "../lib/format";
import { TRANSFORMS, type OutputFormat } from "../lib/transforms";
import type { BatchItem, ClientStatus, JobOutput, Transform } from "../lib/types";
import { usePipeline } from "../lib/usePipeline";

const BG = "oklch(18% 0.012 60)";
const SURF = "oklch(22% 0.014 60)";
const SURF_HI = "oklch(28% 0.014 60)";
const TEXT = "oklch(94% 0.018 80)";
const MUTE = "oklch(70% 0.014 70)";
const FAINT = "oklch(50% 0.012 60)";
const RULE = "oklch(94% 0.018 80 / 0.08)";
const ACCENT = "oklch(75% 0.16 50)";
const ACCENT_DEEP = "oklch(82% 0.18 60)";
const PANEL_PADDING = "20px";

const FORMAT_OPTIONS: Array<{ id: OutputFormat; label: string }> = [
  { id: "webp", label: "WEBP" },
  { id: "avif", label: "AVIF" },
  { id: "jpg", label: "JPEG" },
  { id: "png", label: "PNG" },
];

const VARIANT_OPTIONS: Array<{ id: Transform["name"]; label: string }> = [
  { id: "thumb", label: "Thumbnail" },
  { id: "card", label: "Card" },
  { id: "detail", label: "Detail" },
];

const CROP_BUTTON_WIDTHS: Record<Transform["name"], string> = {
  thumb: "100px",
  card: "78px",
  detail: "86px",
};

const STATUS_SEQUENCE: ClientStatus[] = [
  "presigning",
  "uploading",
  "creating_job",
  "queued",
  "processing",
  "completed",
];

const STATUS_PROGRESS: Record<ClientStatus, number> = {
  pending_submission: 0.04,
  presigning: 0.1,
  uploading: 0.24,
  creating_job: 0.4,
  queued: 0.56,
  processing: 0.82,
  completed: 1,
  failed: 1,
  dead_lettered: 1,
  submission_failed: 1,
  retrying: 0.48,
};

interface LocalFileItem {
  id: string;
  file: File;
  previewURL: string;
  bytes: number;
}

interface BatchSummary {
  label: string;
  progress: number;
  stageIndex: number;
  terminal: boolean;
  allCompleted: boolean;
  failedCount: number;
  completedCount: number;
  totalCount: number;
}

interface DisplayOutput extends JobOutput {
  fileName: string;
  outputFormat: string;
}

function makeId() {
  return Math.random().toString(36).slice(2, 9);
}

function toLocalFile(file: File): LocalFileItem {
  return {
    id: makeId(),
    file,
    previewURL: URL.createObjectURL(file),
    bytes: file.size,
  };
}

function revokePreviews(items: { previewURL: string }[]) {
  items.forEach((item) => URL.revokeObjectURL(item.previewURL));
}

function mono(size: number, color: string): React.CSSProperties {
  return {
    fontFamily: "'JetBrains Mono', 'IBM Plex Mono', monospace",
    letterSpacing: "0.18em",
    textTransform: "uppercase",
    fontSize: `${size}px`,
    color,
  };
}

function isTerminalStatus(status: ClientStatus) {
  return (
    status === "completed" ||
    status === "failed" ||
    status === "dead_lettered" ||
    status === "submission_failed"
  );
}

function isFailureStatus(status: ClientStatus) {
  return status === "failed" || status === "dead_lettered" || status === "submission_failed";
}

function getVariantLabel(name: string) {
  return VARIANT_OPTIONS.find((variant) => variant.id === name)?.label ?? name;
}

function summarizeBatch(items: BatchItem[]): BatchSummary {
  const totalCount = items.length;
  const completedCount = items.filter((item) => item.status === "completed").length;
  const failedCount = items.filter((item) => isFailureStatus(item.status)).length;
  const terminal = items.every((item) => isTerminalStatus(item.status));
  const allCompleted = completedCount === totalCount && totalCount > 0;
  const progress =
    items.reduce((sum, item) => sum + (STATUS_PROGRESS[item.status] ?? 0), 0) /
    Math.max(totalCount, 1);

  let stageIndex = STATUS_SEQUENCE.indexOf("presigning");
  let label = "Queued in browser";

  if (allCompleted) {
    stageIndex = STATUS_SEQUENCE.length - 1;
    label = "Outputs ready";
  } else if (terminal && failedCount > 0) {
    stageIndex = STATUS_SEQUENCE.length - 1;
    label =
      completedCount > 0
        ? `${completedCount} complete · ${failedCount} failed`
        : `${failedCount} ${failedCount === 1 ? "item" : "items"} failed`;
  } else if (items.some((item) => item.status === "processing")) {
    stageIndex = STATUS_SEQUENCE.indexOf("processing");
    label = "Go worker claimed job";
  } else if (items.some((item) => item.status === "queued")) {
    stageIndex = STATUS_SEQUENCE.indexOf("queued");
    label = "Queued in Redis";
  } else if (items.some((item) => item.status === "retrying")) {
    stageIndex = STATUS_SEQUENCE.indexOf("queued");
    label = "Requeueing failed jobs";
  } else if (items.some((item) => item.status === "creating_job")) {
    stageIndex = STATUS_SEQUENCE.indexOf("creating_job");
    label = "Writing job rows";
  } else if (items.some((item) => item.status === "uploading")) {
    stageIndex = STATUS_SEQUENCE.indexOf("uploading");
    label = "Uploading to MinIO";
  } else if (items.some((item) => item.status === "presigning")) {
    stageIndex = STATUS_SEQUENCE.indexOf("presigning");
    label = "Requesting upload URLs";
  } else if (items.some((item) => item.status === "pending_submission")) {
    stageIndex = STATUS_SEQUENCE.indexOf("presigning");
    label = "Queued in browser";
  } else if (items.some((item) => item.status === "submission_failed")) {
    stageIndex = STATUS_SEQUENCE.length - 1;
    label = "Submission failed";
  }

  return {
    label,
    progress,
    stageIndex,
    terminal,
    allCompleted,
    failedCount,
    completedCount,
    totalCount,
  };
}

export function HomePage() {
  const pipeline = usePipeline();
  const [drag, setDrag] = useState(false);
  const [selectedFiles, setSelectedFiles] = useState<LocalFileItem[]>([]);
  const [variants, setVariants] = useState<Set<Transform["name"]>>(
    () => new Set<Transform["name"]>(["thumb", "card", "detail"]),
  );
  const [format, setFormat] = useState<OutputFormat>("webp");
  const selectedFilesRef = useRef(selectedFiles);

  useEffect(() => {
    selectedFilesRef.current = selectedFiles;
  }, [selectedFiles]);

  useEffect(() => {
    return () => {
      revokePreviews(selectedFilesRef.current);
    };
  }, []);

  const transforms = useMemo<Transform[]>(
    () => Array.from(variants).map((variant) => TRANSFORMS[variant]),
    [variants],
  );

  const batchSummary = useMemo(
    () => (pipeline.items.length > 0 ? summarizeBatch(pipeline.items) : null),
    [pipeline.items],
  );

  const outputs = useMemo<DisplayOutput[]>(
    () =>
      pipeline.items.flatMap((item) =>
        item.outputs.map((output) => ({
          ...output,
          fileName: item.fileName,
          outputFormat: item.outputFormat,
        })),
      ),
    [pipeline.items],
  );

  const hasBatch = pipeline.items.length > 0;
  const controlsLocked = pipeline.isSubmitting || hasBatch;
  const canRun = selectedFiles.length > 0 && variants.size > 0 && !controlsLocked;
  const beamProgress = batchSummary?.progress ?? 0;
  const showResults = batchSummary?.allCompleted ?? false;

  const replaceSelection = useCallback((files: File[]) => {
    setSelectedFiles((prev) => {
      revokePreviews(prev);
      return files.map(toLocalFile);
    });
  }, []);

  const appendSelection = useCallback((files: File[]) => {
    setSelectedFiles((prev) => [...prev, ...files.map(toLocalFile)]);
  }, []);

  const clearSelection = useCallback(() => {
    setSelectedFiles((prev) => {
      revokePreviews(prev);
      return [];
    });
  }, []);

  const removeSelectedFile = useCallback((id: string) => {
    setSelectedFiles((prev) => {
      const target = prev.find((item) => item.id === id);
      if (target) URL.revokeObjectURL(target.previewURL);
      return prev.filter((item) => item.id !== id);
    });
  }, []);

  const onDrop = useCallback(
    (e: React.DragEvent) => {
      e.preventDefault();
      setDrag(false);
      const dropped = Array.from(e.dataTransfer.files).filter((file) =>
        file.type.startsWith("image/"),
      );
      if (dropped.length === 0) return;
      if (selectedFilesRef.current.length === 0) replaceSelection(dropped);
      else appendSelection(dropped);
    },
    [appendSelection, replaceSelection],
  );

  const toggleVariant = useCallback((name: Transform["name"]) => {
    setVariants((prev) => {
      const next = new Set(prev);
      if (next.has(name)) next.delete(name);
      else next.add(name);
      return next;
    });
  }, []);

  const handleSubmit = useCallback(async () => {
    if (!canRun) return;
    await pipeline.submit(
      selectedFiles.map((item) => item.file),
      transforms,
      format,
    );
    clearSelection();
  }, [canRun, clearSelection, format, pipeline, selectedFiles, transforms]);

  const handleReset = useCallback(() => {
    pipeline.reset();
    clearSelection();
  }, [clearSelection, pipeline]);

  return (
    <div
      className="relative flex h-screen w-full justify-center overflow-hidden"
      style={{ background: BG, color: TEXT, fontFamily: "Inter, system-ui, sans-serif" }}
    >
      <div
        className="absolute left-0 right-0 top-0 h-[2px]"
        style={{ background: "oklch(94% 0.018 80 / 0.08)" }}
      >
        <div
          style={{
            height: "100%",
            width: `${beamProgress * 100}%`,
            background: `linear-gradient(90deg, transparent, ${ACCENT} 30%, ${ACCENT_DEEP} 70%, transparent)`,
            transition: "width 180ms ease",
            boxShadow: hasBatch ? `0 0 12px ${ACCENT}` : "none",
          }}
        />
      </div>

      <div className="flex w-full max-w-[760px] flex-col px-6 pt-[6vh] pb-6">
        <div className="flex items-center gap-3">
          <svg width="25" height="25" viewBox="0 0 22 22" fill="none" className="hero-mark-enter">
            <circle cx="11" cy="11" r="8" stroke={TEXT} strokeOpacity="0.6" strokeWidth="1" />
            <circle cx="11" cy="11" r="3" fill={ACCENT} />
          </svg>
          <span className="hero-mark-enter text-[16px]" style={{ fontWeight: 500 }}>
            Photon
          </span>
        </div>

        <div className="mt-5">
          <h1
            className="hero-heading-enter tracking-[-0.02em]"
            style={{
              fontFamily: "'Fraunces', serif",
              fontWeight: 300,
              fontSize: "60px",
              lineHeight: 0.95,
            }}
          >
            One image,
            <br />
            <em style={{ fontStyle: "italic", color: ACCENT, fontWeight: 300 }}>
              every variant
            </em>
            <span style={{ color: MUTE }}> you need.</span>
          </h1>
        </div>

        <div className="mt-10 flex h-[250px] flex-col overflow-hidden">
          {showResults ? (
            <ResultStrip outputs={outputs} />
          ) : hasBatch ? (
            <Running items={pipeline.items} summary={batchSummary!} />
          ) : (
            <Drop
              items={selectedFiles}
              drag={drag}
              setDrag={setDrag}
              onDrop={onDrop}
              onReplace={replaceSelection}
              onAppend={appendSelection}
              onRemove={removeSelectedFile}
              onClear={clearSelection}
            />
          )}
        </div>

        <div className="mt-10 flex flex-wrap items-end gap-x-5 gap-y-3">
          <div className="grid gap-y-3">
            <div className="grid grid-cols-[40px_auto] items-center gap-2">
              <span style={mono(11, FAINT)}>
                fmt
              </span>
              <div
                className="inline-flex rounded-md p-0.5"
                style={{ background: SURF, border: `1px solid ${RULE}` }}
              >
                {FORMAT_OPTIONS.map((option) => {
                  const active = format === option.id;
                  return (
                    <button
                      key={option.id}
                      type="button"
                      disabled={controlsLocked}
                      onClick={() => setFormat(option.id)}
                      className="inline-flex min-h-[20px] items-center rounded px-5 py-1 transition-colors disabled:opacity-40"
                      style={{
                        ...mono(10, active ? ACCENT : FAINT),
                        background: active ? SURF_HI : "transparent",
                      }}
                    >
                      {option.label}
                    </button>
                  );
                })}
              </div>
            </div>

            <div className="grid grid-cols-[40px_auto] items-center gap-2">
              <span className="text-right" style={mono(11, FAINT)}>
                crops
              </span>
              <div className="inline-flex flex-wrap items-center gap-1.5">
                {VARIANT_OPTIONS.map((variant) => {
                  const active = variants.has(variant.id);
                  return (
                    <button
                      key={variant.id}
                      type="button"
                      disabled={controlsLocked}
                      onClick={() => toggleVariant(variant.id)}
                      className="inline-flex min-h-[20px] items-center gap-1.5 rounded-md px-2.5 py-1 text-[12px] transition-colors disabled:opacity-40"
                      style={{
                        background: active ? SURF_HI : SURF,
                        color: active ? TEXT : MUTE,
                        border: `1px solid ${active ? "oklch(75% 0.16 50 / 0.5)" : RULE}`,
                        width: CROP_BUTTON_WIDTHS[variant.id],
                        justifyContent: "flex-start",
                      }}
                    >
                      <span
                        className="inline-block h-1.5 w-1.5 rounded-full"
                        style={{ background: active ? ACCENT : FAINT }}
                      />
                      {variant.label}
                    </button>
                  );
                })}
              </div>
            </div>
          </div>

          <div className="ml-auto flex items-center gap-3">
            {batchSummary?.terminal ? (
              <button
                type="button"
                onClick={handleReset}
                className="rounded-md px-4 py-1.5 text-[12px] transition-colors"
                style={{ border: "1px solid oklch(94% 0.018 80 / 0.15)", color: TEXT }}
              >
                Run another
              </button>
            ) : (
              <button
                type="button"
                disabled={!canRun}
                onClick={handleSubmit}
                className="rounded-md px-4 py-1.5 text-[12px] transition-all disabled:cursor-not-allowed disabled:opacity-30"
                style={{
                  background: ACCENT,
                  color: BG,
                  fontWeight: 600,
                  boxShadow: canRun ? `0 0 24px oklch(75% 0.16 50 / 0.4)` : "none",
                }}
              >
                {pipeline.isSubmitting || hasBatch ? "Running…" : "Run pipeline →"}
              </button>
            )}
          </div>
        </div>

        {pipeline.banner && (
          <div
            className="mt-4 rounded-md px-3 py-2"
            style={{
              border: `1px solid ${pipeline.banner.tone === "ok" ? "oklch(75% 0.16 50 / 0.28)" : "oklch(70% 0.18 30 / 0.32)"}`,
              background:
                pipeline.banner.tone === "ok"
                  ? "oklch(75% 0.16 50 / 0.08)"
                  : "oklch(70% 0.18 30 / 0.08)",
            }}
          >
            <span style={mono(10, pipeline.banner.tone === "ok" ? TEXT : ACCENT_DEEP)}>
              {pipeline.banner.message}
            </span>
          </div>
        )}

        <div className="mt-auto" style={{ color: TEXT }}>
          <SiteFooter />
        </div>
      </div>
    </div>
  );
}

function Drop({
  items,
  drag,
  setDrag,
  onDrop,
  onReplace,
  onAppend,
  onRemove,
  onClear,
}: {
  items: LocalFileItem[];
  drag: boolean;
  setDrag: (value: boolean) => void;
  onDrop: (e: React.DragEvent) => void;
  onReplace: (files: File[]) => void;
  onAppend: (files: File[]) => void;
  onRemove: (id: string) => void;
  onClear: () => void;
}) {
  const empty = items.length === 0;
  const inputId = useId();

  const appendFiles = (files: File[]) => {
    if (files.length === 0) return;
    if (empty) onReplace(files);
    else onAppend(files);
  };

  return (
    <label
      onDragOver={(e) => {
        e.preventDefault();
        setDrag(true);
      }}
      onDragLeave={() => setDrag(false)}
      onDrop={onDrop}
      htmlFor={inputId}
      className="flex h-full w-full cursor-pointer overflow-hidden rounded-md transition-all"
      style={{
        border: `1px ${drag ? "solid" : "dashed"} ${drag ? ACCENT : "oklch(94% 0.018 80 / 0.18)"}`,
        background: drag ? "oklch(75% 0.16 50 / 0.05)" : SURF,
        padding: PANEL_PADDING,
      }}
    >
      <input
        id={inputId}
        type="file"
        accept="image/*"
        multiple
        className="sr-only"
        onChange={(e) => {
          const files = Array.from(e.target.files ?? []);
          appendFiles(files);
          e.currentTarget.value = "";
        }}
      />
      {empty ? (
        <div className="flex w-full flex-col items-center justify-center gap-1 text-center">
          <div
            className="text-[22px]"
            style={{
              fontFamily: "'Fraunces', serif",
              fontStyle: "italic",
              fontWeight: 300,
              color: TEXT,
              lineHeight: 1,
            }}
          >
            Drop your image
          </div>
          <div className="mt-2" style={mono(10, FAINT)}>
            or click to browse · max 50&nbsp;mb
          </div>
        </div>
      ) : (
        <div className="grid h-full min-h-0 w-full grid-rows-[auto,minmax(0,1fr)]">
          <div className="flex min-h-[62px] items-center justify-between gap-4">
            <div className="min-w-0">
              <div
                className="text-[18px]"
                style={{
                  fontFamily: "'Fraunces', serif",
                  fontStyle: "italic",
                  fontWeight: 300,
                  color: TEXT,
                }}
              >
                {items.length} image{items.length === 1 ? "" : "s"} selected
              </div>
              <div className="mt-1" style={mono(10, FAINT)}>
                {formatBytes(items.reduce((sum, item) => sum + item.bytes, 0))} total
              </div>
            </div>
            <button
              type="button"
              onClick={(e) => {
                e.preventDefault();
                e.stopPropagation();
                onClear();
              }}
              className="rounded-md px-3 py-1.5 transition-colors"
              style={{ border: `1px solid ${RULE}`, ...mono(10, FAINT) }}
            >
              Clear all
            </button>
          </div>

          <div className="mt-3 min-h-0 overflow-hidden">
            <div className="styled-scrollbar h-full overflow-y-scroll overflow-x-hidden pr-2">
              <div className="flex flex-wrap content-start gap-3">
                {items.map((item) => (
                  <div key={item.id} className="relative shrink-0">
                    <img
                      src={item.previewURL}
                      alt={item.file.name}
                      className="h-20 w-20 rounded-md object-cover"
                      style={{ border: `1px solid ${RULE}` }}
                    />
                    <button
                      type="button"
                      aria-label={`Remove ${item.file.name}`}
                      onClick={(e) => {
                        e.preventDefault();
                        e.stopPropagation();
                        onRemove(item.id);
                      }}
                      className="absolute right-1 top-1 grid h-6 w-6 place-items-center rounded-full transition-colors"
                      style={{
                        background: "oklch(18% 0.012 60 / 0.88)",
                        border: `1px solid oklch(94% 0.018 80 / 0.18)`,
                        color: TEXT,
                        fontSize: "12px",
                        lineHeight: 1,
                      }}
                    >
                      ×
                    </button>
                  </div>
                ))}

                <span
                  className="grid h-20 w-20 shrink-0 place-items-center rounded-md"
                  style={{ border: "1px dashed oklch(94% 0.018 80 / 0.18)", ...mono(10, FAINT) }}
                >
                  + add
                </span>
              </div>
            </div>
          </div>
        </div>
      )}
    </label>
  );
}

function Running({ items, summary }: { items: BatchItem[]; summary: BatchSummary }) {
  return (
    <div
      className="flex h-full w-full flex-col overflow-hidden rounded-md"
      style={{ background: SURF, border: `1px solid ${RULE}`, padding: PANEL_PADDING }}
    >
      <div className="grid min-h-[62px] grid-rows-[auto,auto]">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <span
              className="inline-block h-1.5 w-1.5 rounded-full"
              style={{
                background: summary.failedCount > 0 ? ACCENT_DEEP : ACCENT,
                boxShadow: `0 0 8px ${summary.failedCount > 0 ? ACCENT_DEEP : ACCENT}`,
                animation: summary.terminal ? "none" : "designPulse 1.4s ease-in-out infinite",
              }}
            />
            <span style={mono(10, TEXT)}>{summary.label}</span>
          </div>
          <span style={mono(10, FAINT)}>
            {summary.completedCount}/{summary.totalCount}
          </span>
        </div>
        <div className="mt-3 grid grid-cols-6 gap-1 self-start">
          {STATUS_SEQUENCE.map((status, index) => {
            const active = index <= summary.stageIndex;
            return (
              <div
                key={status}
                className="h-[3px] rounded-full"
                style={{
                  background: active ? ACCENT : "oklch(94% 0.018 80 / 0.1)",
                  transition: "background 200ms",
                }}
              />
            );
          })}
        </div>
      </div>
      <div className="mt-3 min-h-0 overflow-hidden">
        <div className="styled-scrollbar h-full overflow-y-scroll overflow-x-hidden pr-2">
          <div className="flex flex-wrap content-start gap-3">
            {items.map((item) => (
              <img
                key={item.clientID}
                src={item.previewURL}
                alt={item.fileName}
                className="h-20 w-20 rounded-md object-cover"
                style={{
                  border: `1px solid ${
                    isFailureStatus(item.status)
                      ? "oklch(70% 0.18 30 / 0.55)"
                      : item.status === "completed"
                        ? "oklch(75% 0.16 50 / 0.35)"
                        : RULE
                  }`,
                }}
              />
            ))}
          </div>
        </div>
      </div>
      <style>{`@keyframes designPulse { 0%,100%{ opacity:0.4 } 50%{ opacity:1 } }`}</style>
    </div>
  );
}

function ResultStrip({ outputs }: { outputs: DisplayOutput[] }) {
  return (
    <div
      className="flex h-full w-full flex-col overflow-hidden rounded-md"
      style={{ background: SURF, border: `1px solid ${RULE}`, padding: PANEL_PADDING }}
    >
      <div className="flex items-baseline justify-between gap-4">
        <span className="text-[14px]" style={{ fontFamily: "'Fraunces', serif", color: TEXT }}>
          {outputs.length} outputs ready.
        </span>
        <span style={mono(10, FAINT)}>presigned downloads</span>
      </div>
      <div className="mt-3 min-h-0 overflow-hidden">
        <div className="styled-scrollbar h-full overflow-y-scroll overflow-x-hidden pr-2">
          <div className="flex flex-wrap gap-1.5">
            {outputs.map((output) => (
              <a
                key={`${output.job_id}-${output.id}`}
                href={output.download_url}
                target="_blank"
                rel="noreferrer"
                className="inline-flex items-center gap-1.5 rounded-md px-2.5 py-1 no-underline transition-colors"
                style={{ background: SURF_HI, border: `1px solid ${RULE}`, fontSize: "11px", color: TEXT }}
              >
                <span style={mono(9, ACCENT)}>{output.outputFormat}</span>
                <span style={{ fontWeight: 500 }}>{getVariantLabel(output.variant_name)}</span>
                <span style={{ color: FAINT }}>{formatBytes(output.size_bytes)}</span>
              </a>
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}
