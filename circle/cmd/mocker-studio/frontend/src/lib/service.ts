/**
 * Mocker Studio Service 绑定层（wails v2）。
 *
 * 直接 re-export `wails generate module` 生成的强类型 bindings，
 * 不要再手写 FNV-1a 哈希派发。
 */
import {
  ApplyEdit,
  BuildGraph,
  Compile,
  GetWorkspace,
  InspectNode,
  LoadFile,
  LocateNode,
  OpenWorkspace,
  ParseFile,
  ParseSource,
  ParseWorkspace,
  ReparseWorkspace,
  Run,
  SaveFile,
  SerializeToSource,
  SetWorkspace,
  Version,
} from "../../wailsjs/go/ide/Service";

export {
  ApplyEdit,
  BuildGraph,
  Compile,
  GetWorkspace,
  InspectNode,
  LoadFile,
  LocateNode,
  OpenWorkspace,
  ParseFile,
  ParseSource,
  ParseWorkspace,
  ReparseWorkspace,
  Run,
  SaveFile,
  SerializeToSource,
  SetWorkspace,
  Version,
};
