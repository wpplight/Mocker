export namespace ide {
	
	export class BlockState {
	    name: string;
	    status: string;
	    trigger?: string;
	
	    static createFrom(source: any = {}) {
	        return new BlockState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.status = source["status"];
	        this.trigger = source["trigger"];
	    }
	}
	export class CompileOptions {
	    outputPath: string;
	    emitGo: string;
	    keepTmp: boolean;
	    run: boolean;
	    runArgs: string;
	    source: string;
	    workspace: string;
	
	    static createFrom(source: any = {}) {
	        return new CompileOptions(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.outputPath = source["outputPath"];
	        this.emitGo = source["emitGo"];
	        this.keepTmp = source["keepTmp"];
	        this.run = source["run"];
	        this.runArgs = source["runArgs"];
	        this.source = source["source"];
	        this.workspace = source["workspace"];
	    }
	}
	export class CompileResult {
	    success: boolean;
	    output: string;
	    error?: string;
	    exitCode: number;
	    generatedGo?: string;
	
	    static createFrom(source: any = {}) {
	        return new CompileResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.output = source["output"];
	        this.error = source["error"];
	        this.exitCode = source["exitCode"];
	        this.generatedGo = source["generatedGo"];
	    }
	}
	export class Diagnostic {
	    line: number;
	    column: number;
	    message: string;
	    hint?: string;
	
	    static createFrom(source: any = {}) {
	        return new Diagnostic(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.line = source["line"];
	        this.column = source["column"];
	        this.message = source["message"];
	        this.hint = source["hint"];
	    }
	}
	export class EdgeDetail {
	    src: string;
	    edge: string;
	    dst: string;
	    body: string[];
	
	    static createFrom(source: any = {}) {
	        return new EdgeDetail(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.src = source["src"];
	        this.edge = source["edge"];
	        this.dst = source["dst"];
	        this.body = source["body"];
	    }
	}
	export class Edit {
	    op: string;
	    payload: any;
	
	    static createFrom(source: any = {}) {
	        return new Edit(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.op = source["op"];
	        this.payload = source["payload"];
	    }
	}
	export class EnumDetail {
	    name: string;
	    values: string[];
	
	    static createFrom(source: any = {}) {
	        return new EnumDetail(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.values = source["values"];
	    }
	}
	export class FileInfo {
	    path: string;
	    absPath: string;
	    pkg: string;
	    size: number;
	
	    static createFrom(source: any = {}) {
	        return new FileInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.absPath = source["absPath"];
	        this.pkg = source["pkg"];
	        this.size = source["size"];
	    }
	}
	export class FlowEdge {
	    id: string;
	    source: string;
	    target: string;
	    sourceHandle?: string;
	    targetHandle?: string;
	    edgeName: string;
	    animated: boolean;
	    srcPkg: string;
	    dstPkg: string;
	    crossPackage: boolean;
	    kind: string;
	    data?: Record<string, any>;
	
	    static createFrom(source: any = {}) {
	        return new FlowEdge(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.source = source["source"];
	        this.target = source["target"];
	        this.sourceHandle = source["sourceHandle"];
	        this.targetHandle = source["targetHandle"];
	        this.edgeName = source["edgeName"];
	        this.animated = source["animated"];
	        this.srcPkg = source["srcPkg"];
	        this.dstPkg = source["dstPkg"];
	        this.crossPackage = source["crossPackage"];
	        this.kind = source["kind"];
	        this.data = source["data"];
	    }
	}
	export class FlowNode {
	    id: string;
	    type: string;
	    name: string;
	    qualifiedName: string;
	    pkg: string;
	    exported: boolean;
	    isBoundary: boolean;
	    collapseState: string;
	    position: Record<string, number>;
	    data: Record<string, any>;
	
	    static createFrom(source: any = {}) {
	        return new FlowNode(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.type = source["type"];
	        this.name = source["name"];
	        this.qualifiedName = source["qualifiedName"];
	        this.pkg = source["pkg"];
	        this.exported = source["exported"];
	        this.isBoundary = source["isBoundary"];
	        this.collapseState = source["collapseState"];
	        this.position = source["position"];
	        this.data = source["data"];
	    }
	}
	export class PackageInfo {
	    name: string;
	    isMain: boolean;
	    nodeIds: string[];
	    boundaryNodeIds: string[];
	    defaultCollapsed: boolean;
	
	    static createFrom(source: any = {}) {
	        return new PackageInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.isMain = source["isMain"];
	        this.nodeIds = source["nodeIds"];
	        this.boundaryNodeIds = source["boundaryNodeIds"];
	        this.defaultCollapsed = source["defaultCollapsed"];
	    }
	}
	export class GraphData {
	    nodes: FlowNode[];
	    edges: FlowEdge[];
	    packages: PackageInfo[];
	
	    static createFrom(source: any = {}) {
	        return new GraphData(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.nodes = this.convertValues(source["nodes"], FlowNode);
	        this.edges = this.convertValues(source["edges"], FlowEdge);
	        this.packages = this.convertValues(source["packages"], PackageInfo);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class IRNodeState {
	    nodeName: string;
	    inputs?: Record<string, any>;
	    outputs?: Record<string, any>;
	    blocks: BlockState[];
	
	    static createFrom(source: any = {}) {
	        return new IRNodeState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.nodeName = source["nodeName"];
	        this.inputs = source["inputs"];
	        this.outputs = source["outputs"];
	        this.blocks = this.convertValues(source["blocks"], BlockState);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class NodeMember {
	    kind: string;
	    name: string;
	    type?: string;
	    value?: string;
	
	    static createFrom(source: any = {}) {
	        return new NodeMember(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.name = source["name"];
	        this.type = source["type"];
	        this.value = source["value"];
	    }
	}
	export class NodeDetail {
	    name: string;
	    exported: boolean;
	    kind: string;
	    members: NodeMember[];
	
	    static createFrom(source: any = {}) {
	        return new NodeDetail(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.exported = source["exported"];
	        this.kind = source["kind"];
	        this.members = this.convertValues(source["members"], NodeMember);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class NodeLocation {
	    path: string;
	    line: number;
	    col: number;
	    name: string;
	
	    static createFrom(source: any = {}) {
	        return new NodeLocation(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.line = source["line"];
	        this.col = source["col"];
	        this.name = source["name"];
	    }
	}
	
	
	export class ParsedFile {
	    packageName: string;
	    imports: string[];
	    graph: GraphData;
	    nodes: NodeDetail[];
	    edges: EdgeDetail[];
	    enums: EnumDetail[];
	    errors?: Diagnostic[];
	
	    static createFrom(source: any = {}) {
	        return new ParsedFile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.packageName = source["packageName"];
	        this.imports = source["imports"];
	        this.graph = this.convertValues(source["graph"], GraphData);
	        this.nodes = this.convertValues(source["nodes"], NodeDetail);
	        this.edges = this.convertValues(source["edges"], EdgeDetail);
	        this.enums = this.convertValues(source["enums"], EnumDetail);
	        this.errors = this.convertValues(source["errors"], Diagnostic);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class VersionInfo {
	    app: string;
	    build: string;
	    circleDir: string;
	    goVersion: string;
	
	    static createFrom(source: any = {}) {
	        return new VersionInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.app = source["app"];
	        this.build = source["build"];
	        this.circleDir = source["circleDir"];
	        this.goVersion = source["goVersion"];
	    }
	}
	export class WorkspaceInfo {
	    root: string;
	    pkgName: string;
	    mainFile: string;
	    mainSource: string;
	    files: FileInfo[];
	    graph: GraphData;
	    parsed?: ParsedFile;
	    errors: Diagnostic[];
	
	    static createFrom(source: any = {}) {
	        return new WorkspaceInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.root = source["root"];
	        this.pkgName = source["pkgName"];
	        this.mainFile = source["mainFile"];
	        this.mainSource = source["mainSource"];
	        this.files = this.convertValues(source["files"], FileInfo);
	        this.graph = this.convertValues(source["graph"], GraphData);
	        this.parsed = this.convertValues(source["parsed"], ParsedFile);
	        this.errors = this.convertValues(source["errors"], Diagnostic);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

