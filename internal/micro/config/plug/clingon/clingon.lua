-- clingon's checks, as you type: whenever a pack's file changes it's checked
-- the way `clingon check` checks it. Each issue is marked in the gutter at its
-- line, and the status bar shows it as an orange ISSUE chip: the one on the
-- cursor's line, or else the first. The gutter's room is kept even with no
-- issues (an unseen mark on line 1), so the text never shifts as they come
-- and go.
VERSION = "1.0.0"

local micro = import("micro")
local shell = import("micro/shell")
local buffer = import("micro/buffer")
local os = import("os")
local ioutil = import("io/ioutil")
local strings = import("strings")
local fmt = import("fmt")
local filepath = import("path/filepath")

-- clingon says where it is when it opens micro; without it, nothing to do.
local exe = os.Getenv("CLINGON_CHECK")
local scratch = filepath.Join(os.TempDir(), "clingon-check-" .. os.Getpid() .. ".md")

local issues = {} -- by line: what's wrong there
local first = nil -- the first issue's line
local checked = nil -- the text last checked

local function ours(buf)
	return exe ~= "" and strings.HasSuffix(buf.Path, ".md")
end

local function check(buf)
	local text = buf:Bytes()
	ioutil.WriteFile(scratch, text, 384) -- what's on screen, saved or not
	local out, err = shell.ExecCommand(exe, "_check-file", buf.AbsPath, scratch)
	buf:ClearMessages("clingon")
	issues, first = {}, nil
	if err ~= nil then
		return
	end
	local lines = strings.Split(strings.TrimSpace(out), "\n") -- a Go list: counted with #, from 1
	for i = 1, #lines do
		local parts = strings.SplitN(lines[i], "\t", 2)
		if #parts == 2 then
			local n = tonumber(parts[1]) -- Lua's own: strconv.Atoi's two results don't come through
			if n ~= nil and issues[n] == nil then
				issues[n] = parts[2]
				if first == nil then
					first = n
				end
				-- No text of its own: micro would show it unstyled. The chip says it.
				buf:AddMessage(buffer.NewMessageAtLine("clingon", "", n, buffer.MTError))
			end
		end
	end
end

-- show puts the issue on the status bar, as a chip. micro clears it on every
-- event, so it's put back after each one.
local function show(bp)
	if first == nil then
		return
	end
	local here = bp.Cursor.Y + 1
	if issues[here] ~= nil then
		micro.InfoBar():Error("ISSUE: " .. issues[here])
	else
		micro.InfoBar():Error("ISSUE: line " .. first .. ": " .. issues[first])
	end
end

function onBufPaneOpen(bp)
	if not ours(bp.Buf) then
		return
	end
	bp.Buf:AddMessage(buffer.NewMessageAtLine("clingon-gutter", "", 1, buffer.MTInfo))
	checked = fmt.Sprintf("%s", bp.Buf:Bytes())
	check(bp.Buf)
end

function onAnyEvent()
	local bp = micro.CurPane()
	if bp == nil or bp.Buf == nil or not ours(bp.Buf) then
		return
	end
	-- As a Lua string: Go's byte slices can't be compared from here.
	local text = fmt.Sprintf("%s", bp.Buf:Bytes())
	if checked == nil or text ~= checked then
		checked = text
		check(bp.Buf)
	end
	show(bp)
end

-- The copy of the unsaved text goes when micro does.
function onQuit(bp)
	os.Remove(scratch)
	return true
end
