import { useState, useMemo } from "react";
import { useEditorStore } from "../store/editor";
import * as svc from "../lib/service";
import { cn } from "../lib/utils";
import { ScrollArea } from "./ui/scroll-area";
import { Separator } from "./ui/separator";
import {
  Box,
  ArrowRightLeft,
  Hash,
  ChevronRight,
  ChevronDown,
  Package,
  Import,
  Search,
  Eye,
  FileCode2,
  Folder,
  RefreshCw,
  Layers,
} from "lucide-react";

export function Sidebar() {
  const {
    sidebarCollapsed,
    parsed,
    selectedNodeId,
    setSelectedNodeId,
    workspaceFiles,
    workspaceRoot,
    currentFile,
    setCurrentFile,
    // M1: 跨包折叠
    packages,
    collapsedPackages,
    togglePackageCollapse,
  } = useEditorStore();
  const [searchQuery, setSearchQuery] = useState("");
  const [sectionsOpen, setSectionsOpen] = useState<Record<string, boolean>>({
    files: true,
    package: true,
    packages: true,
    nodes: true,
    enums: true,
    edges: true,
  });

  const toggleSection = (key: string) => {
    setSectionsOpen((prev) => ({ ...prev, [key]: !prev[key] }));
  };

  const lowerQuery = searchQuery.toLowerCase();

  const filteredNodes = useMemo(() => {
    if (!parsed) return [];
    return parsed.nodes
      .filter((n) => n.kind === "node" || n.kind === "struct")
      .filter((n) => !lowerQuery || n.name.toLowerCase().includes(lowerQuery));
  }, [parsed, lowerQuery]);

  const filteredEnums = useMemo(() => {
    if (!parsed) return [];
    return parsed.enums
      .filter((e) => !lowerQuery || e.name.toLowerCase().includes(lowerQuery));
  }, [parsed, lowerQuery]);

  const filteredEdges = useMemo(() => {
    if (!parsed) return [];
    return parsed.edges
      .filter((e) =>
        !lowerQuery ||
        e.src.toLowerCase().includes(lowerQuery) ||
        e.edge.toLowerCase().includes(lowerQuery) ||
        e.dst.toLowerCase().includes(lowerQuery)
      );
  }, [parsed, lowerQuery]);

  // 切换文件（点击文件树）
  const handleFileClick = async (path: string) => {
    try {
      const content = await svc.LoadFile(path);
      setCurrentFile(path, content);
    } catch (err) {
      console.error("LoadFile failed:", err);
    }
  };

  // 重新加载 workspace
  const handleReloadWorkspace = async () => {
    if (!workspaceRoot) return;
    try {
      const info = await svc.OpenWorkspace(workspaceRoot);
      useEditorStore.getState().setWorkspace(info);
      if (info.errors) {
        useEditorStore.getState().setDiagnostics(info.errors);
      }
      if (info.parsed) {
        useEditorStore.getState().setParsed(info.parsed);
      }
    } catch (err) {
      console.error("Reload failed:", err);
    }
  };

  if (sidebarCollapsed) return null;

  return (
    <div className="w-60 h-full border-r border-[var(--border)] bg-[var(--card)]/40 flex flex-col shrink-0">
      {/* Search input */}
      <div className="p-2 border-b border-[var(--border)]/50">
        <div className="relative">
          <Search className="absolute left-2 top-1/2 -translate-y-1/2 w-3 h-3 text-[var(--muted-foreground)]/50" />
          <input
            type="text"
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            placeholder="Filter..."
            className="w-full pl-7 pr-2 py-1 text-xs font-mono bg-[var(--background)] border border-[var(--border)]/50 rounded-md text-[var(--foreground)] placeholder:text-[var(--muted-foreground)]/30 focus:outline-none focus:border-[var(--primary)]/40 focus:ring-1 focus:ring-[var(--primary)]/20 transition-colors"
          />
        </div>
      </div>

      <ScrollArea className="flex-1">
        <div className="p-3 space-y-4">
          {/* M0.5: 工作区文件树 */}
          {workspaceRoot && (
            <div className="space-y-1">
              <div className="flex items-center gap-1.5">
                <CollapsibleSection
                  icon={<Folder className="w-3 h-3" />}
                  label="Files"
                  open={sectionsOpen.files}
                  onToggle={() => toggleSection("files")}
                  count={workspaceFiles.length}
                />
                <button
                  onClick={handleReloadWorkspace}
                  className="ml-auto p-0.5 hover:bg-[var(--accent)]/50 rounded transition-colors"
                  title="Reload workspace"
                >
                  <RefreshCw className="w-2.5 h-2.5 text-[var(--muted-foreground)]/50 hover:text-[var(--muted-foreground)]" />
                </button>
              </div>
              {sectionsOpen.files && (
                <div className="text-[10px] font-mono text-[var(--muted-foreground)]/40 pl-5 py-0.5 truncate">
                  {workspaceRoot}
                </div>
              )}
              {sectionsOpen.files && (
                <div className="space-y-0.5">
                  {workspaceFiles.map((f) => (
                    <button
                      key={f.path}
                      onClick={() => handleFileClick(f.path)}
                      className={cn(
                        "group w-full flex items-center gap-1.5 pl-5 py-1 rounded-md text-xs transition-all cursor-pointer text-left",
                        currentFile === f.path
                          ? "bg-[var(--primary)]/10 text-[var(--foreground)]"
                          : "text-[var(--muted-foreground)] hover:text-[var(--foreground)] hover:bg-[var(--accent)]/50"
                      )}
                      title={f.absPath}
                    >
                      <FileCode2 className="w-3 h-3 shrink-0 text-[var(--muted-foreground)]/50" />
                      <span className="font-mono truncate">{f.path}</span>
                      <span className="ml-auto text-[9px] text-[var(--muted-foreground)]/30 font-mono">
                        {f.pkg}
                      </span>
                    </button>
                  ))}
                  {workspaceFiles.length === 0 && (
                    <div className="text-[10px] text-[var(--muted-foreground)]/30 pl-5 py-0.5 italic">
                      no .ce files
                    </div>
                  )}
                </div>
              )}
            </div>
          )}

          {workspaceRoot && <Separator />}

          {/* Package info */}
          {parsed && sectionsOpen.package !== undefined && (
            <div className="space-y-1">
              <CollapsibleSection
                icon={<Package className="w-3 h-3" />}
                label="Package"
                open={sectionsOpen.package}
                onToggle={() => toggleSection("package")}
              />
              {sectionsOpen.package && (
                <>
                  <div className="text-xs font-mono text-[var(--primary)] pl-5 py-0.5">
                    {parsed.packageName || "(unnamed)"}
                  </div>
                  {(parsed.imports ?? []).length > 0 && (
                    <>
                      <CollapsibleSection
                        icon={<Import className="w-3 h-3" />}
                        label="Imports"
                        open={true}
                        count={(parsed.imports ?? []).length}
                      />
                      {(parsed.imports ?? []).map((imp) => (
                        <div
                          key={imp}
                          className="text-xs font-mono text-[var(--muted-foreground)] pl-5 py-0.5"
                        >
                          {imp}
                        </div>
                      ))}
                    </>
                  )}
                </>
              )}
            </div>
          )}

          <Separator />

          {/* M1: Packages section（跨包折叠入口） */}
          {packages.length > 0 && (
            <div className="space-y-1">
              <CollapsibleSection
                icon={<Layers className="w-3 h-3" />}
                label="Packages"
                open={sectionsOpen.packages}
                onToggle={() => toggleSection("packages")}
                count={packages.length}
              />
              {sectionsOpen.packages && (
                <div className="space-y-0.5 pl-2">
                  {packages.map((p) => {
                    const isCollapsed = collapsedPackages[p.name] ?? p.defaultCollapsed;
                    return (
                      <div
                        key={p.name || "(main)"}
                        className={cn(
                          "flex items-center gap-1.5 py-0.5 px-1.5 rounded-sm text-xs font-mono",
                          "hover:bg-[var(--accent)]/30 transition-colors"
                        )}
                      >
                        <button
                          onClick={() => togglePackageCollapse(p.name)}
                          className={cn(
                            "shrink-0 w-4 h-4 flex items-center justify-center rounded-sm",
                            isCollapsed
                              ? "text-[var(--primary)] bg-[var(--primary)]/10"
                              : "text-[var(--muted-foreground)]"
                          )}
                          title={isCollapsed ? "展开包" : "折叠包"}
                        >
                          {isCollapsed ? (
                            <ChevronRight className="w-3 h-3" />
                          ) : (
                            <ChevronDown className="w-3 h-3" />
                          )}
                        </button>
                        <Package className={cn(
                          "w-3 h-3 shrink-0",
                          p.isMain ? "text-[var(--primary)]" : "text-[var(--edge-color)]"
                        )} />
                        <span className="text-[var(--foreground)] truncate flex-1">
                          {p.name || "(main)"}
                        </span>
                        <span className="text-[10px] text-[var(--muted-foreground)] shrink-0">
                          {p.nodeIds.length}
                          {p.boundaryNodeIds.length > 0 && p.boundaryNodeIds.length < p.nodeIds.length && (
                            <span className="text-[var(--primary)] ml-1">
                              ·{p.boundaryNodeIds.length}⇄
                            </span>
                          )}
                        </span>
                      </div>
                    );
                  })}
                </div>
              )}
            </div>
          )}

          <Separator />

          {/* Nodes section */}
          <div className="space-y-1">
            <CollapsibleSection
              icon={<Box className="w-3 h-3" />}
              label="Nodes"
              open={sectionsOpen.nodes}
              onToggle={() => toggleSection("nodes")}
              count={filteredNodes.length}
            />
            {sectionsOpen.nodes && (
              <>
                {filteredNodes.map((n) => (
                  <SidebarItem
                    key={n.name}
                    name={n.name}
                    badge={n.kind}
                    exported={n.exported}
                    selected={selectedNodeId === `node-${n.name}`}
                    onClick={() => setSelectedNodeId(`node-${n.name}`)}
                    memberCount={n.members.length}
                  />
                ))}
                {filteredNodes.length === 0 && <EmptyLabel />}
              </>
            )}
          </div>

          <Separator />

          {/* Enums section */}
          <div className="space-y-1">
            <CollapsibleSection
              icon={<Hash className="w-3 h-3" />}
              label="Enums"
              open={sectionsOpen.enums}
              onToggle={() => toggleSection("enums")}
              count={filteredEnums.length}
            />
            {sectionsOpen.enums && (
              <>
                {filteredEnums.map((e) => (
                  <SidebarItem
                    key={e.name}
                    name={e.name}
                    badge="enum"
                    selected={selectedNodeId === `enum-${e.name}`}
                    onClick={() => setSelectedNodeId(`enum-${e.name}`)}
                    memberCount={e.values.length}
                  />
                ))}
                {filteredEnums.length === 0 && <EmptyLabel />}
              </>
            )}
          </div>

          <Separator />

          {/* Edges section */}
          <div className="space-y-1">
            <CollapsibleSection
              icon={<ArrowRightLeft className="w-3 h-3" />}
              label="Edges"
              open={sectionsOpen.edges}
              onToggle={() => toggleSection("edges")}
              count={filteredEdges.length}
            />
            {sectionsOpen.edges && (
              <>
                {filteredEdges.map((e) => (
                  <div
                    key={`${e.src}-${e.edge}-${e.dst}`}
                    className="group flex items-center gap-1.5 pl-5 py-0.5 text-xs font-mono text-[var(--muted-foreground)] hover:text-[var(--foreground)] hover:bg-[var(--accent)]/30 rounded-sm transition-colors cursor-default"
                  >
                    <Eye className="w-2.5 h-2.5 opacity-0 group-hover:opacity-40 shrink-0 transition-opacity text-[var(--muted-foreground)]" />
                    <span className="text-[var(--edge-color)]/60 truncate">{e.src}</span>
                    <span className="text-[var(--muted-foreground)]/40">{"<"}</span>
                    <span className="text-[var(--foreground)]">{e.edge}</span>
                    <span className="text-[var(--muted-foreground)]/40">{">"}</span>
                    <span className="text-[var(--edge-color)]/60 truncate">{e.dst}</span>
                  </div>
                ))}
                {filteredEdges.length === 0 && <EmptyLabel />}
              </>
            )}
          </div>
        </div>
      </ScrollArea>
    </div>
  );
}

function CollapsibleSection({
  icon,
  label,
  open,
  onToggle,
  count,
}: {
  icon: React.ReactNode;
  label: string;
  open: boolean;
  onToggle?: () => void;
  count?: number;
}) {
  return (
    <button
      onClick={onToggle}
      className="flex items-center gap-1.5 w-full text-[10px] font-medium uppercase tracking-wider text-[var(--muted-foreground)]/60 hover:text-[var(--muted-foreground)] transition-colors cursor-pointer"
    >
      {open ? (
        <ChevronDown className="w-3 h-3 shrink-0 transition-transform" />
      ) : (
        <ChevronRight className="w-3 h-3 shrink-0 transition-transform" />
      )}
      {icon}
      {label}
      {count !== undefined && (
        <span className="ml-auto text-[9px] text-[var(--muted-foreground)]/30 font-mono">
          {count}
        </span>
      )}
    </button>
  );
}

function SidebarItem({
  name,
  badge,
  exported,
  selected,
  onClick,
  memberCount,
}: {
  name: string;
  badge: string;
  exported?: boolean;
  selected: boolean;
  onClick: () => void;
  memberCount: number;
}) {
  return (
    <button
      onClick={onClick}
      className={cn(
        "group w-full flex items-center gap-1.5 pl-5 py-1 rounded-md text-xs transition-all cursor-pointer text-left",
        selected
          ? "bg-[var(--primary)]/10 text-[var(--foreground)]"
          : "text-[var(--muted-foreground)] hover:text-[var(--foreground)] hover:bg-[var(--accent)]/50"
      )}
    >
      <ChevronRight
        className={cn(
          "w-3 h-3 shrink-0 transition-transform",
          selected && "rotate-90"
        )}
      />
      <Eye className="w-2.5 h-2.5 shrink-0 opacity-0 group-hover:opacity-40 transition-opacity text-[var(--muted-foreground)]" />
      {exported && (
        <span className="text-[var(--primary)]/60 text-[10px] font-mono">@</span>
      )}
      <span className="font-mono font-medium truncate">{name}</span>
      <span className="ml-auto flex items-center gap-1">
        <span className="text-[10px] text-[var(--muted-foreground)]/40">{memberCount}</span>
        <span
          className={cn(
            "text-[9px] font-semibold px-1.5 py-0 rounded-full",
            badge === "node"
              ? "bg-[var(--primary)]/20 text-[var(--primary)]"
              : badge === "struct"
              ? "bg-[var(--port-out)]/20 text-[var(--port-out)]"
              : badge === "enum"
              ? "bg-[var(--muted-foreground)]/20 text-[var(--muted-foreground)]"
              : "bg-[var(--edge-color)]/20 text-[var(--edge-color)]"
          )}
        >
          {badge}
        </span>
      </span>
    </button>
  );
}

function EmptyLabel() {
  return (
    <div className="text-[10px] text-[var(--muted-foreground)]/30 pl-5 py-0.5 italic">
      none
    </div>
  );
}
