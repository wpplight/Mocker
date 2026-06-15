import type { Monaco } from "@monaco-editor/react";

const LANGUAGE_ID = "mocker";

const mockerLanguageDef: import("monaco-editor").languages.IMonarchLanguage = {
  defaultToken: "",
  keywords: ["package", "import", "enum", "for", "while", "if", "main"],
  typeKeywords: ["str", "num", "bool", "byte", "any", "int", "float"],
  operators: [":=", ">>", "=", "+=", "-=", "*", "+", "-", "<", ">", "<=", ">=", "!=", "=="],

  symbols: /[=><!~?:&|+\-*\/\^%]+/,

  tokenizer: {
    root: [
      // single-line comment
      [/\/\/.*$/, "comment"],

      // double-quoted string
      [/"([^"\\]|\\.)*$/, "string.invalid"],
      [/"/, { token: "string.quote", bracket: "@open", next: "@string" }],

      // @Name — exported node declaration
      [/@[a-zA-Z_]\w*/, "keyword.export"],

      // <edge-name> — edge reference
      [/<[a-zA-Z_][a-zA-Z0-9_]*>/, "type.edge"],

      // SYSCALL keyword
      [/\bSYSCALL\b/, "keyword.syscall"],

      // identifiers and keywords
      [
        /[a-zA-Z_]\w*/,
        {
          cases: {
            "@keywords": "keyword",
            "@typeKeywords": "type",
            "@default": "identifier",
          },
        },
      ],

      // := operator (must come before single : )
      [/:=/, "operator.assign"],

      // >> operator
      [/>>/, "operator.flow"],

      // comparison and arithmetic operators
      [/[<>=!]=?/, "operator"],
      [/[+\-*/]/, "operator"],
      [/[,;.:]/, "delimiter"],

      // numbers
      [/\d+(\.\d+)?/, "number"],
    ],

    string: [
      [/[^\\"]+/, "string"],
      [/\\./, "string.escape"],
      [/"/, { token: "string.quote", bracket: "@close", next: "@pop" }],
    ],
  },
};

const mockerTheme: import("monaco-editor").editor.IStandaloneThemeData = {
  base: "vs-dark",
  inherit: true,
  rules: [
    { token: "comment", foreground: "5a5a78", fontStyle: "italic" },
    { token: "keyword", foreground: "7c6bf0", fontStyle: "bold" },
    { token: "keyword.export", foreground: "e08040", fontStyle: "bold" },
    { token: "keyword.syscall", foreground: "7c6bf0", fontStyle: "bold" },
    { token: "type", foreground: "4db8c4" },
    { token: "type.edge", foreground: "d4a04e" },
    { token: "string", foreground: "5ab87a" },
    { token: "string.quote", foreground: "5ab87a" },
    { token: "string.escape", foreground: "e08040" },
    { token: "number", foreground: "e08040" },
    { token: "operator.assign", foreground: "e08040" },
    { token: "operator.flow", foreground: "e08040" },
    { token: "operator", foreground: "e08040" },
    { token: "delimiter", foreground: "e8e8f0" },
    { token: "identifier", foreground: "e8e8f0" },
  ],
  colors: {
    "editor.background": "#1a1a2e",
    "editor.foreground": "#e8e8f0",
    "editorCursor.foreground": "#e8e8f0",
    "editor.lineHighlightBackground": "#22223a",
    "editor.selectionBackground": "#33335a",
    "editorLineNumber.foreground": "#5a5a78",
    "editorLineNumber.activeForeground": "#e8e8f0",
    "editor.inactiveSelectionBackground": "#2a2a48",
  },
};

export function registerMockerLanguage(monaco: Monaco): void {
  // Register the language
  monaco.languages.register({
    id: LANGUAGE_ID,
    extensions: [".ce"],
    aliases: ["Mocker", "mocker", "ce"],
  });

  // Set the tokenizer
  monaco.languages.setMonarchTokensProvider(LANGUAGE_ID, mockerLanguageDef);

  // Define the theme
  monaco.editor.defineTheme("mocker-dark", mockerTheme);
}

export { LANGUAGE_ID };
