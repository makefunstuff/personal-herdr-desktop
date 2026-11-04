-- herdr_theme: Neovim colours that follow the Herdr layout on screen (workbench, fortress, dos, pixel, ...).
-- Reads ~/.config/herdr-app: shared.json "layout" -> layouts/<layout>.json root "theme" -> themes/<theme>.json,
-- builds a palette from that theme's terminal colours, and re-applies live when the layout is switched.
-- Without Herdr it falls back to the workbench palette. Used by colors/herdr.lua.
local M = {}
local uv = vim.uv or vim.loop
local HOME = vim.env.HERDR_APP_HOME or (vim.env.HOME .. '/.config/herdr-app')

local WORKBENCH = {
  background = '#0d0f10', foreground = '#d7dcd9', selection = '#23404a',
  ansi = { '#0d0f10', '#e0574a', '#7fc46a', '#ffb000', '#4aa3d8', '#b07ad8', '#4fd1c5', '#c9cfcc',
    '#4a5257', '#ff7a6b', '#a6e08f', '#ffcc4d', '#7fc4f0', '#cfa0f0', '#8be8de', '#ffffff' },
  colors = { accent = '#ffb000', border = '#4e5b57', muted = '#8fa3a8', dim = '#66727a' },
}

local function read(path)
  local f = io.open(path, 'r')
  if not f then return nil end
  local s = f:read('*a'); f:close()
  local ok, v = pcall(vim.json.decode, s)
  return ok and v or nil
end

-- colour helpers
local function rgb(h) h = h:gsub('#', ''); return tonumber(h:sub(1, 2), 16), tonumber(h:sub(3, 4), 16), tonumber(h:sub(5, 6), 16) end
local function hex(r, g, b) return string.format('#%02x%02x%02x', math.floor(r + .5), math.floor(g + .5), math.floor(b + .5)) end
local function mix(a, b, t) local r1, g1, b1 = rgb(a); local r2, g2, b2 = rgb(b); return hex(r1 + (r2 - r1) * t, g1 + (g2 - g1) * t, b1 + (b2 - b1) * t) end
local function lum(h) local r, g, b = rgb(h); local function ch(v) v = v / 255; return v <= .03928 and v / 12.92 or ((v + .055) / 1.055) ^ 2.4 end; return .2126 * ch(r) + .7152 * ch(g) + .0722 * ch(b) end
local function contrast(a, b) local x, y = lum(a), lum(b); if x < y then x, y = y, x end; return (x + .05) / (y + .05) end
local function readable(col, bg, min) return col and contrast(col, bg) >= (min or 3) and col or nil end

function M.current()
  local shared = read(HOME .. '/shared.json') or {}
  local layout = shared.layout or 'workbench'
  local L = read(HOME .. '/layouts/' .. layout .. '.json') or {}
  local name = type(L.theme) == 'table' and (vim.o.background == 'light' and L.theme.light or L.theme.dark) or L.theme
  local theme = name and read(HOME .. '/themes/' .. name .. '.json') or nil
  return layout, name or 'workbench', theme or WORKBENCH
end

function M.palette(t)
  local A, C = t.ansi or WORKBENCH.ansi, t.colors or {}
  local bg, fg = t.background or WORKBENCH.background, t.foreground or WORKBENCH.foreground
  local accent = readable(C.accent, bg, 3) or A[12] or A[4]
  return {
    bg = bg, bg_dark = mix(bg, '#000000', .35), panel = mix(bg, fg, .07), raised = mix(bg, fg, .12), hover = mix(bg, fg, .16),
    sel = readable(t.selection, fg, 1.5) and t.selection or mix(bg, accent, .30), line = readable(C.border, bg, 1.6) or mix(bg, fg, .30),
    line_dim = mix(bg, fg, .16), fg = fg, fg_bright = A[16] or '#ffffff', muted = readable(C.muted, bg, 3) or mix(bg, fg, .65),
    dim = readable(C.dim, bg, 2.2) or mix(bg, fg, .45), faint = mix(bg, fg, .30),
    amber = accent, amber_hi = A[12] or accent, amber_dim = mix(bg, accent, .45), orange = A[4] ~= accent and A[4] or A[12],
    green = A[3], green_hi = A[11], cyan = A[7], cyan_hi = A[15], blue = A[5], blue_hi = A[13], violet = A[6],
    red = A[2], red_hi = A[10], silk = A[8], ansi = A,
  }
end

function M.apply()
  local layout, name, theme = M.current()
  local c = M.palette(theme)
  vim.cmd 'highlight clear'
  if vim.fn.exists 'syntax_on' == 1 then vim.cmd 'syntax reset' end
  vim.o.background = lum(c.bg) > .4 and 'light' or 'dark'
  vim.o.termguicolors = true
  vim.g.colors_name = 'herdr'
  M.layout, M.theme = layout, name
  local groups = {
    -- Editor
    Normal = { fg = c.fg, bg = c.bg },
    NormalNC = { fg = c.fg, bg = c.bg },
    NormalFloat = { fg = c.fg, bg = c.panel },
    FloatBorder = { fg = c.line, bg = c.panel },
    FloatTitle = { fg = c.bg, bg = c.amber, bold = true },
    FloatFooter = { fg = c.dim, bg = c.panel },
    WinSeparator = { fg = c.line, bg = c.bg },
    VertSplit = { link = 'WinSeparator' },
    Cursor = { fg = c.bg, bg = c.amber },
    lCursor = { link = 'Cursor' },
    TermCursor = { link = 'Cursor' },
    CursorLine = { bg = '#15191a' },
    CursorColumn = { link = 'CursorLine' },
    ColorColumn = { bg = c.panel },
    LineNr = { fg = c.faint },
    LineNrAbove = { fg = c.faint },
    LineNrBelow = { fg = c.faint },
    CursorLineNr = { fg = c.amber, bold = true },
    SignColumn = { fg = c.dim, bg = c.bg },
    FoldColumn = { fg = c.faint, bg = c.bg },
    Folded = { fg = c.muted, bg = c.panel },
    EndOfBuffer = { fg = c.bg },
    NonText = { fg = c.line_dim },
    Whitespace = { fg = c.line_dim },
    SpecialKey = { fg = c.line },
    Conceal = { fg = c.dim },
    Visual = { bg = c.sel },
    VisualNOS = { link = 'Visual' },
    Search = { fg = c.bg, bg = c.amber_hi },
    IncSearch = { fg = c.bg, bg = c.amber, bold = true },
    CurSearch = { link = 'IncSearch' },
    Substitute = { fg = c.bg, bg = c.orange },
    MatchParen = { fg = c.amber, bg = c.raised, bold = true },
    Pmenu = { fg = c.fg, bg = c.panel },
    PmenuSel = { fg = c.bg, bg = c.amber, bold = true },
    PmenuSbar = { bg = c.raised },
    PmenuThumb = { bg = c.line },
    PmenuKind = { fg = c.cyan, bg = c.panel },
    PmenuExtra = { fg = c.dim, bg = c.panel },
    WildMenu = { link = 'PmenuSel' },
    StatusLine = { fg = c.fg, bg = c.panel },
    StatusLineNC = { fg = c.dim, bg = c.bg_dark },
    TabLine = { fg = c.dim, bg = c.bg_dark },
    TabLineSel = { fg = c.bg, bg = c.amber, bold = true },
    TabLineFill = { bg = c.bg_dark },
    WinBar = { fg = c.muted, bg = c.bg, bold = true },
    WinBarNC = { fg = c.dim, bg = c.bg },
    Title = { fg = c.amber, bold = true },
    Directory = { fg = c.blue_hi },
    ModeMsg = { fg = c.amber, bold = true },
    MoreMsg = { fg = c.green },
    Question = { fg = c.amber },
    MsgArea = { fg = c.fg },
    ErrorMsg = { fg = c.red_hi, bold = true },
    WarningMsg = { fg = c.amber },
    QuickFixLine = { bg = c.sel, bold = true },
    SpellBad = { sp = c.red, undercurl = true },
    SpellCap = { sp = c.amber, undercurl = true },
    SpellRare = { sp = c.violet, undercurl = true },
    SpellLocal = { sp = c.cyan, undercurl = true },
    DiffAdd = { bg = '#16281b' },
    DiffChange = { bg = '#1c2328' },
    DiffDelete = { fg = '#5a2620', bg = '#1e1210' },
    DiffText = { bg = '#2b3a20', bold = true },

    -- Syntax
    Comment = { fg = c.dim, italic = true },
    Constant = { fg = c.amber_hi },
    String = { fg = c.green },
    Character = { fg = c.green_hi },
    Number = { fg = c.amber_hi },
    Boolean = { fg = c.orange, bold = true },
    Float = { link = 'Number' },
    Identifier = { fg = c.fg },
    Function = { fg = c.cyan },
    Statement = { fg = c.amber },
    Conditional = { fg = c.amber },
    Repeat = { fg = c.amber },
    Label = { fg = c.amber },
    Operator = { fg = c.silk },
    Keyword = { fg = c.amber },
    Exception = { fg = c.red_hi },
    PreProc = { fg = c.violet },
    Include = { fg = c.violet },
    Define = { fg = c.violet },
    Macro = { fg = c.violet },
    PreCondit = { fg = c.violet },
    Type = { fg = c.blue_hi },
    StorageClass = { fg = c.amber },
    Structure = { fg = c.blue_hi },
    Typedef = { fg = c.blue_hi },
    Special = { fg = c.orange },
    SpecialChar = { fg = c.orange },
    Tag = { fg = c.amber },
    Delimiter = { fg = c.muted },
    SpecialComment = { fg = c.muted, italic = true },
    Debug = { fg = c.red },
    Underlined = { underline = true },
    Ignore = { fg = c.faint },
    Error = { fg = c.red_hi },
    Todo = { fg = c.bg, bg = c.amber, bold = true },
    Added = { fg = c.green },
    Changed = { fg = c.amber },
    Removed = { fg = c.red },

    -- Treesitter
    ['@variable'] = { fg = c.fg },
    ['@variable.builtin'] = { fg = c.orange, italic = true },
    ['@variable.parameter'] = { fg = c.silk, italic = true },
    ['@variable.member'] = { fg = c.blue_hi },
    ['@property'] = { fg = c.blue_hi },
    ['@constant'] = { fg = c.amber_hi },
    ['@constant.builtin'] = { fg = c.orange, bold = true },
    ['@constant.macro'] = { fg = c.violet },
    ['@module'] = { fg = c.cyan_hi },
    ['@label'] = { fg = c.amber },
    ['@string'] = { fg = c.green },
    ['@string.escape'] = { fg = c.orange },
    ['@string.regexp'] = { fg = c.orange },
    ['@string.special'] = { fg = c.cyan_hi },
    ['@string.special.url'] = { fg = c.blue_hi, underline = true },
    ['@character'] = { fg = c.green_hi },
    ['@number'] = { fg = c.amber_hi },
    ['@boolean'] = { fg = c.orange, bold = true },
    ['@type'] = { fg = c.blue_hi },
    ['@type.builtin'] = { fg = c.blue, italic = true },
    ['@type.definition'] = { fg = c.blue_hi, bold = true },
    ['@attribute'] = { fg = c.violet },
    ['@function'] = { fg = c.cyan },
    ['@function.builtin'] = { fg = c.cyan_hi, italic = true },
    ['@function.call'] = { fg = c.cyan },
    ['@function.macro'] = { fg = c.violet },
    ['@function.method'] = { fg = c.cyan },
    ['@function.method.call'] = { fg = c.cyan },
    ['@constructor'] = { fg = c.blue_hi, bold = true },
    ['@operator'] = { fg = c.silk },
    ['@keyword'] = { fg = c.amber },
    ['@keyword.function'] = { fg = c.amber, bold = true },
    ['@keyword.return'] = { fg = c.amber, bold = true },
    ['@keyword.import'] = { fg = c.violet },
    ['@keyword.exception'] = { fg = c.red_hi },
    ['@keyword.operator'] = { fg = c.amber },
    ['@punctuation.delimiter'] = { fg = c.muted },
    ['@punctuation.bracket'] = { fg = c.muted },
    ['@punctuation.special'] = { fg = c.orange },
    ['@comment'] = { link = 'Comment' },
    ['@comment.todo'] = { link = 'Todo' },
    ['@comment.note'] = { fg = c.bg, bg = c.cyan, bold = true },
    ['@comment.warning'] = { fg = c.bg, bg = c.orange, bold = true },
    ['@comment.error'] = { fg = c.bg, bg = c.red, bold = true },
    ['@tag'] = { fg = c.amber },
    ['@tag.attribute'] = { fg = c.blue_hi, italic = true },
    ['@tag.delimiter'] = { fg = c.muted },
    ['@markup.heading'] = { fg = c.amber, bold = true },
    ['@markup.heading.1'] = { fg = c.amber, bold = true },
    ['@markup.heading.2'] = { fg = c.amber_hi, bold = true },
    ['@markup.heading.3'] = { fg = c.cyan, bold = true },
    ['@markup.heading.4'] = { fg = c.green, bold = true },
    ['@markup.strong'] = { bold = true },
    ['@markup.italic'] = { italic = true },
    ['@markup.strikethrough'] = { strikethrough = true },
    ['@markup.link'] = { fg = c.blue_hi },
    ['@markup.link.url'] = { fg = c.blue_hi, underline = true },
    ['@markup.raw'] = { fg = c.green_hi },
    ['@markup.list'] = { fg = c.amber },
    ['@markup.quote'] = { fg = c.muted, italic = true },
    ['@diff.plus'] = { fg = c.green },
    ['@diff.minus'] = { fg = c.red },
    ['@diff.delta'] = { fg = c.amber },
    ['@lsp.type.namespace'] = { link = '@module' },
    ['@lsp.type.parameter'] = { link = '@variable.parameter' },
    ['@lsp.type.property'] = { link = '@property' },
    ['@lsp.type.enumMember'] = { link = '@constant' },
    ['@lsp.mod.deprecated'] = { strikethrough = true },

    -- Diagnostics and LSP
    DiagnosticError = { fg = c.red_hi },
    DiagnosticWarn = { fg = c.amber },
    DiagnosticInfo = { fg = c.blue_hi },
    DiagnosticHint = { fg = c.cyan },
    DiagnosticOk = { fg = c.green },
    DiagnosticUnderlineError = { sp = c.red, undercurl = true },
    DiagnosticUnderlineWarn = { sp = c.amber, undercurl = true },
    DiagnosticUnderlineInfo = { sp = c.blue, undercurl = true },
    DiagnosticUnderlineHint = { sp = c.cyan, undercurl = true },
    DiagnosticVirtualTextError = { fg = c.red, bg = '#1e1210' },
    DiagnosticVirtualTextWarn = { fg = c.amber, bg = '#1e1808' },
    DiagnosticVirtualTextInfo = { fg = c.blue, bg = '#0f1a22' },
    DiagnosticVirtualTextHint = { fg = c.cyan, bg = '#0f1e1c' },
    LspReferenceText = { bg = c.raised },
    LspReferenceRead = { bg = c.raised },
    LspReferenceWrite = { bg = c.raised, underline = true },
    LspInlayHint = { fg = c.faint, italic = true },
    LspSignatureActiveParameter = { fg = c.amber, bold = true, underline = true },
    LspInfoBorder = { link = 'FloatBorder' },

    -- Git
    GitSignsAdd = { fg = c.green },
    GitSignsChange = { fg = c.amber },
    GitSignsDelete = { fg = c.red },
    GitSignsCurrentLineBlame = { fg = c.faint, italic = true },
    diffAdded = { fg = c.green },
    diffRemoved = { fg = c.red },
    diffChanged = { fg = c.amber },
    diffFile = { fg = c.amber, bold = true },
    diffLine = { fg = c.cyan },

    -- Telescope: amber-titled modules on slate panels
    TelescopeNormal = { fg = c.fg, bg = c.panel },
    TelescopeBorder = { fg = c.line, bg = c.panel },
    TelescopePromptNormal = { fg = c.fg, bg = c.raised },
    TelescopePromptBorder = { fg = c.amber, bg = c.raised },
    TelescopePromptTitle = { fg = c.bg, bg = c.amber, bold = true },
    TelescopePromptPrefix = { fg = c.amber },
    TelescopePreviewTitle = { fg = c.bg, bg = c.cyan, bold = true },
    TelescopeResultsTitle = { fg = c.bg, bg = c.line, bold = true },
    TelescopeSelection = { fg = c.fg_bright, bg = c.sel, bold = true },
    TelescopeSelectionCaret = { fg = c.amber, bg = c.sel },
    TelescopeMatching = { fg = c.amber, bold = true },

    -- Completion (blink.cmp)
    BlinkCmpMenu = { fg = c.fg, bg = c.panel },
    BlinkCmpMenuBorder = { fg = c.line, bg = c.panel },
    BlinkCmpMenuSelection = { fg = c.bg, bg = c.amber, bold = true },
    BlinkCmpLabelMatch = { fg = c.amber_hi, bold = true },
    BlinkCmpKind = { fg = c.cyan },
    BlinkCmpSource = { fg = c.dim },
    BlinkCmpLabelDescription = { fg = c.dim },
    BlinkCmpDoc = { fg = c.fg, bg = c.panel },
    BlinkCmpDocBorder = { fg = c.line, bg = c.panel },
    BlinkCmpSignatureHelp = { fg = c.fg, bg = c.panel },
    BlinkCmpSignatureHelpBorder = { fg = c.line, bg = c.panel },
    BlinkCmpGhostText = { fg = c.faint, italic = true },

    -- which-key, noice, trouble, fidget
    WhichKey = { fg = c.amber, bold = true },
    WhichKeyGroup = { fg = c.cyan },
    WhichKeyDesc = { fg = c.fg },
    WhichKeySeparator = { fg = c.faint },
    WhichKeyNormal = { bg = c.panel },
    WhichKeyBorder = { fg = c.line, bg = c.panel },
    WhichKeyTitle = { fg = c.bg, bg = c.amber, bold = true },
    NoiceCmdlinePopup = { fg = c.fg, bg = c.panel },
    NoiceCmdlinePopupBorder = { fg = c.amber, bg = c.panel },
    NoiceCmdlinePopupTitle = { fg = c.bg, bg = c.amber, bold = true },
    NoiceCmdlineIcon = { fg = c.amber },
    NoiceMini = { fg = c.muted, bg = c.bg_dark },
    TroubleNormal = { fg = c.fg, bg = c.bg_dark },
    TroubleNormalNC = { fg = c.fg, bg = c.bg_dark },
    FidgetTitle = { fg = c.amber, bold = true },
    FidgetTask = { fg = c.dim },

    -- mini.statusline: the mode block is the amber module label
    MiniStatuslineModeNormal = { fg = c.bg, bg = c.amber, bold = true },
    MiniStatuslineModeInsert = { fg = c.bg, bg = c.green, bold = true },
    MiniStatuslineModeVisual = { fg = c.bg, bg = c.cyan, bold = true },
    MiniStatuslineModeReplace = { fg = c.bg, bg = c.red, bold = true },
    MiniStatuslineModeCommand = { fg = c.bg, bg = c.amber_hi, bold = true },
    MiniStatuslineModeOther = { fg = c.bg, bg = c.violet, bold = true },
    MiniStatuslineDevinfo = { fg = c.fg, bg = c.raised },
    MiniStatuslineFilename = { fg = c.muted, bg = c.panel },
    MiniStatuslineFileinfo = { fg = c.fg, bg = c.raised },
    MiniStatuslineInactive = { fg = c.dim, bg = c.bg_dark },
    MiniSurround = { fg = c.bg, bg = c.amber },

    -- indent-blankline, render-markdown, todo-comments, dap
    IblIndent = { fg = '#1c2224', nocombine = true },
    IblScope = { fg = c.amber_dim, nocombine = true },
    IblWhitespace = { fg = '#1c2224', nocombine = true },
    RenderMarkdownH1Bg = { bg = '#2a1f08' },
    RenderMarkdownH2Bg = { bg = '#221b0c' },
    RenderMarkdownH3Bg = { bg = '#0f1f1d' },
    RenderMarkdownCode = { bg = c.panel },
    RenderMarkdownCodeInline = { fg = c.green_hi, bg = c.panel },
    RenderMarkdownBullet = { fg = c.amber },
    RenderMarkdownTableHead = { fg = c.amber },
    RenderMarkdownTableRow = { fg = c.line },
    DapBreakpoint = { fg = c.red_hi },
    DapStopped = { fg = c.amber },
    DapStoppedLine = { bg = '#2a1f08' },
  }

  -- Transparent editor background: the Herdr pane's own background (and its texture/opacity) shows through.
  if vim.g.herdr_theme_opaque ~= true then
    for _, g in ipairs { 'Normal', 'NormalNC', 'SignColumn', 'FoldColumn', 'EndOfBuffer', 'LineNr', 'CursorLineNr' } do
      groups[g] = vim.tbl_extend('force', groups[g] or {}, { bg = 'NONE' })
    end
  end
  for group, spec in pairs(groups) do vim.api.nvim_set_hl(0, group, spec) end
  for i, col in ipairs(c.ansi) do vim.g['terminal_color_' .. (i - 1)] = col end
  vim.api.nvim_exec_autocmds('ColorScheme', { pattern = 'herdr', modeline = false })
end

-- Re-apply when the Herdr layout (or its theme file) changes. shared.json is replaced atomically, so the folder is watched.
function M.watch()
  -- one watcher per Neovim, even when the scheme is re-applied
  if _G.__herdr_theme_watcher or not uv.fs_stat(HOME) then return end
  M.watcher = uv.new_fs_event(); _G.__herdr_theme_watcher = M.watcher
  local pending = false
  M.watcher:start(HOME, {}, function(_, file)
    if pending or not file or not (file == 'shared.json' or file:match '^themes' or file:match '^layouts') then return end
    pending = true
    vim.defer_fn(function()
      pending = false
      if vim.g.colors_name ~= 'herdr' then return end
      local mod = require 'herdr_theme'
      local layout, name = mod.current()
      if layout ~= mod.layout or name ~= mod.theme then mod.apply() end
    end, 300)
  end)
end

return M
