-- Workbench: square module frames, amber when active, slate when not (matches the Herdr workbench layout).
hl.config({
  general = {
    border_size = 2,
    col = {
      active_border = "rgb(ffb000)",
      inactive_border = "rgb(4e5b57)",
    },
  },

  decoration = {
    rounding = 0,
    shadow = { enabled = false },
  },

  group = {
    col = {
      border_active = "rgb(ffb000)",
      border_inactive = "rgb(4e5b57)",
    },
  },
})
