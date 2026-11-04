-- herdr-nvim on Herdr Desktop. The plugin talks to the legacy `herdr` server (`herdr agent list`, `herdr agent
-- prompt`), which Herdr Desktop doesn't run; there the agents live in herdr-core and answer to `hc`. This swaps the
-- plugin's agent list and send for hc ones, scoped to the Herdr space this Neovim runs in (its terminal's `where`,
-- "<space> / <tab>", from HC_TERMINAL_ID). Outside Herdr Desktop (no HC_TERMINAL_ID) the plugin is left as it is.
local M = {}

local AGENTS = { ['claude-code'] = 'claude', claude = 'claude', codex = 'codex', pi = 'pi', opencode = 'opencode', gemini = 'gemini' }

local function hc_ls()
  local r = vim.system({ 'hc', 'ls' }, { text = true }):wait()
  if r.code ~= 0 then return nil, 'hc ls failed: ' .. ((r.stderr or '') ~= '' and r.stderr or ('exit ' .. r.code)) end
  local ok, d = pcall(vim.json.decode, r.stdout or '')
  if not ok or type(d) ~= 'table' then return nil, 'hc ls: unparseable JSON' end
  return d.terminals or {}
end

-- "INF-9 ST-18395 iam / agent" -> "INF-9 ST-18395 iam", "agent" (the last " / " splits; space names may hold one).
local function split_where(w)
  w = w or ''
  local s, t = w:match('^(.*) / ([^/]*)$')
  return s or w, t or ''
end

function M.setup()
  local me = vim.env.HC_TERMINAL_ID
  if not me or me == '' then return end
  local ok_a, agents = pcall(require, 'herdr-nvim.agents')
  local ok_d, dispatch = pcall(require, 'herdr-nvim.dispatch')
  if not (ok_a and ok_d) then return end

  agents.list = function()
    local ts, err = hc_ls()
    if not ts then return nil, err end
    local space, tab
    for _, t in ipairs(ts) do
      if t.address == me then space, tab = split_where(t.where) end
    end
    if not space then return nil, 'this Neovim\'s terminal (' .. me .. ') is not in hc ls' end
    local out = {}
    for _, t in ipairs(ts) do
      local kind = AGENTS[t.agent or '']
      local s, tb = split_where(t.where)
      if kind and t.state == 'live' and s == space and t.address ~= me then
        table.insert(out, {
          pane_id = t.address, -- the hc address; dispatch.send below takes it
          workspace_id = space,
          tab_id = tb,
          kind = kind,
          status = t.status or 'unknown',
          cwd = t.cwd or '',
          title = tb ~= '' and tb or kind,
          same_tab = tb == tab,
        })
      end
    end
    table.sort(out, function(x, y) return x.title < y.title end)
    return out
  end

  -- The one meant without asking: this tab's agent, else the space's only one, else the space's "agent" tab
  -- (Herdr task spaces: the worker beside its nvim tab). Otherwise nil, and the plugin shows its picker.
  agents.resolve = function(list)
    local function only(f)
      local hit
      for _, a in ipairs(list) do
        if f(a) then
          if hit then return nil end
          hit = a
        end
      end
      return hit
    end
    return only(function(a) return a.same_tab end) or (#list == 1 and list[1]) or only(function(a) return a.tab_id == 'agent' end)
  end

  agents.display = function(a)
    return string.format('%s · %s · %s', a.kind, a.status, a.tab_id ~= '' and a.tab_id or vim.fn.fnamemodify(a.cwd, ':t'))
  end

  dispatch.send = function(addr, text, opts)
    opts = opts or {}
    if opts.submit then
      -- hc send pastes it and presses Enter (it waits for a pause in the person's typing first).
      local r = vim.system({ 'hc', 'send', addr, text }, { text = true }):wait()
      if r.code ~= 0 then return false, 'hc send failed: ' .. ((r.stderr or '') ~= '' and r.stderr or ('exit ' .. r.code)) end
      return true
    end
    -- Without submitting: hc can't type without Enter, so the text goes to the clipboard to paste into the agent.
    vim.fn.setreg('+', text)
    vim.notify('herdr-nvim: copied for ' .. addr .. ' (paste it into the agent)', vim.log.levels.INFO)
    return true
  end
end

return M
