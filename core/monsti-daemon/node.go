// This file is part of Monsti, a web content management system.
// Copyright 2012-2013 Christian Neumann
//
// Monsti is free software: you can redistribute it and/or modify it under the
// terms of the GNU Affero General Public License as published by the Free
// Software Foundation, either version 3 of the License, or (at your option) any
// later version.
//
// Monsti is distributed in the hope that it will be useful, but WITHOUT ANY
// WARRANTY; without even the implied warranty of MERCHANTABILITY or FITNESS FOR
// A PARTICULAR PURPOSE.  See the GNU Affero General Public License for more
// details.
//
// You should have received a copy of the GNU Affero General Public License
// along with Monsti.  If not, see <http://www.gnu.org/licenses/>.

package main

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"

	"github.com/quirkey/magick"
	"pkg.monsti.org/form"
	"pkg.monsti.org/gettext"
	"pkg.monsti.org/monsti/api/service"
	"pkg.monsti.org/monsti/api/util"
	mtemplate "pkg.monsti.org/monsti/api/util/template"
)

// navLink represents a link in the navigation.
type navLink struct {
	Name, Target  string
	Active, Child bool
	Order         int
}

type navigation []navLink

// Len is the number of elements in the navigation.
func (n navigation) Len() int {
	return len(n)
}

// Less returns whether the element with index i should sort
// before the element with index j.
func (n navigation) Less(i, j int) bool {
	return n[i].Order < n[j].Order || (n[i].Order == n[j].Order &&
		n[i].Name < n[j].Name)
}

// Swap swaps the elements with indexes i and j.
func (n *navigation) Swap(i, j int) {
	(*n)[i], (*n)[j] = (*n)[j], (*n)[i]
}

type getNodeFunc func(path string) (*service.Node, error)
type getChildrenFunc func(path string) ([]*service.Node, error)

// getNav returns the navigation for the given node.
//
// nodePath is the absolute path of the node for which to get the navigation.
// active is the absolute path to the currently active node.
func getNav(nodePath, active string,
	getNodeFn getNodeFunc, getChildrenFn getChildrenFunc) (
	navLinks navigation, err error) {
	// Search children
	children, err := getChildrenFn(nodePath)
	if err != nil {
		return nil, fmt.Errorf("Could not get children: %v", err)
	}
	childrenNavLinks := navLinks[:]
	for _, child := range children {
		if child.Hide {
			continue
		}
		title := "Untitled"
		if child.Fields["core.Title"] != nil {
			title = child.Fields["core.Title"].String()
		}
		childrenNavLinks = append(childrenNavLinks, navLink{
			Name:   title,
			Target: path.Join(nodePath, child.Name()),
			Child:  true, Order: child.Order})
	}
	if len(childrenNavLinks) == 0 {
		if nodePath == "/" || path.Dir(nodePath) == "/" {
			return nil, nil
		}
		return getNav(path.Dir(nodePath), active, getNodeFn, getChildrenFn)
	}
	sort.Sort(&childrenNavLinks)
	siblingsNavLinks := navLinks[:]
	// Search siblings
	if nodePath != "/" && path.Dir(nodePath) == "/" {
		node, err := getNodeFn(nodePath)
		if err != nil {
			return nil, fmt.Errorf("Could not get node: %v", err)
		}
		siblingsNavLinks = append(siblingsNavLinks, navLink{
			Name:   node.Fields["core.Title"].String(),
			Target: nodePath, Order: node.Order})
	} else if nodePath != "/" {
		parent := path.Dir(nodePath)
		siblings, err := getChildrenFn(parent)
		if err != nil {
			return nil, fmt.Errorf("Could not get siblings: %v", err)
		}
		for _, sibling := range siblings {
			if sibling.Hide {
				continue
			}
			siblingsNavLinks = append(siblingsNavLinks, navLink{
				Name:   sibling.Fields["core.Title"].String(),
				Target: path.Join(nodePath, "..", sibling.Name()), Order: sibling.Order})
		}
	}
	sort.Sort(&siblingsNavLinks)
	// Insert children at their parent
	for i, link := range siblingsNavLinks {
		if link.Target == nodePath {
			navLinks = append(navLinks, siblingsNavLinks[:i+1]...)
			navLinks = append(navLinks, childrenNavLinks...)
			navLinks = append(navLinks, siblingsNavLinks[i+1:]...)
			break
		}
	}
	if len(navLinks) == 0 {
		navLinks = childrenNavLinks
	}
	// Compute node paths relative to active node and search and set the Active
	// link
	for i, link := range navLinks {
		if strings.Contains(active, link.Target) && path.Dir(active) != link.Target {
			navLinks[i].Active = true
		}
	}
	return
}

// MakeAbsolute converts relative targets to absolute ones by adding the given
// root path. It also adds a trailing slash.
func (nav *navigation) MakeAbsolute(root string) {
	for i := range *nav {
		if (*nav)[i].Target[0] != '/' {
			(*nav)[i].Target = path.Join(root, (*nav)[i].Target)
		}
		(*nav)[i].Target = (*nav)[i].Target + "/"
	}
}

type addFormData struct {
	NodeType string
	New      string
}

// Add handles add requests.
func (h *nodeHandler) Add(c *reqContext) error {
	G, _, _, _ := gettext.DefaultLocales.Use("", c.UserSession.Locale)
	data := addFormData{New: "1"}
	nodeTypeOptions := []form.Option{}
	nodeTypes, err := c.Serv.Monsti().GetAddableNodeTypes(c.Site.Name,
		c.Node.Type.Id)
	if err != nil {
		return fmt.Errorf("Could not get addable node types: %v", err)
	}
	for _, nodeType := range nodeTypes {
		nodeTypeOptions = append(nodeTypeOptions,
			form.Option{nodeType, nodeType})
	}
	selectWidget := form.SelectWidget{nodeTypeOptions}
	form := form.NewForm(&data, []form.Field{
		form.Field{"NodeType", G("Content type"), "", form.Required(G("Required.")),
			selectWidget},
		form.Field{"New", "", "", nil, new(form.HiddenWidget)},
	})
	form.Action = path.Join(c.Node.Path, "@@edit")
	body, err := h.Renderer.Render("actions/addform", mtemplate.Context{
		"Form": form.RenderData()}, c.UserSession.Locale,
		h.Settings.Monsti.GetSiteTemplatesPath(c.Site.Name))
	if err != nil {
		return fmt.Errorf("Can't render node add formular: %v", err)
	}
	env := masterTmplEnv{Node: c.Node, Session: c.UserSession,
		Flags: EDIT_VIEW, Title: G("Add content")}
	fmt.Fprint(c.Res, renderInMaster(h.Renderer, []byte(body), env, h.Settings,
		*c.Site, c.UserSession.Locale, c.Serv))
	return nil
}

type removeFormData struct {
	Confirm int
}

// Remove handles remove requests.
func (h *nodeHandler) Remove(c *reqContext) error {
	G, _, _, _ := gettext.DefaultLocales.Use("", c.UserSession.Locale)
	data := removeFormData{}
	form := form.NewForm(&data, []form.Field{
		form.Field{"Confirm", G("Confirm"), "", form.Required(G("Required.")),
			new(form.HiddenWidget)}})
	switch c.Req.Method {
	case "GET":
	case "POST":
		if err := c.Req.ParseForm(); err != nil {
			return err
		}
		if form.Fill(c.Req.Form) {
			if err := c.Serv.Monsti().RemoveNode(c.Site.Name, c.Node.Path); err != nil {
				return fmt.Errorf("Could not remove node: %v", err)
			}
			http.Redirect(c.Res, c.Req, path.Dir(c.Node.Path), http.StatusSeeOther)
			return nil
		}
	default:
		return fmt.Errorf("Request method not supported: %v", c.Req.Method)
	}
	data.Confirm = 1489
	body, err := h.Renderer.Render("actions/removeform", mtemplate.Context{
		"Form": form.RenderData(), "Node": c.Node},
		c.UserSession.Locale, h.Settings.Monsti.GetSiteTemplatesPath(c.Site.Name))
	if err != nil {
		panic("Can't render node remove formular: " + err.Error())
	}
	env := masterTmplEnv{Node: c.Node, Session: c.UserSession,
		Flags: EDIT_VIEW, Title: fmt.Sprintf(G("Remove \"%v\""), c.Node.Name())}
	fmt.Fprint(c.Res, renderInMaster(h.Renderer, []byte(body), env, h.Settings,
		*c.Site, c.UserSession.Locale, c.Serv))
	return nil
}

type imageSize struct{ Width, Height int }

func (s imageSize) String() string {
	return fmt.Sprintf("%vx%v", s.Width, s.Height)
}

// ViewNode handles node views.
func (h *nodeHandler) View(c *reqContext) error {
	h.Log.Printf("(%v) %v %v", c.Site.Name, c.Req.Method, c.Req.URL.Path)

	if err := c.Req.ParseForm(); err != nil {
		return err
	}

	// Redirect if trailing slash is missing and if this is not a file
	// node (in which case we write out the file's content).
	if c.Node.Path[len(c.Node.Path)-1] != '/' {
		if c.Node.Type.Id == "core.Image" {
			sizeName := c.Req.FormValue("size")
			var size imageSize
			var body []byte
			var err error
			if sizeName != "" {
				err = c.Serv.Monsti().GetConfig(c.Site.Name, "image", "sizes."+sizeName,
					&size)
				if err != nil || size.Width == 0 {
					if err != nil {
						h.Log.Printf("Could not get size config: %v", err)
					} else {
						h.Log.Printf("Could not find size %q for site %q: %v", sizeName,
							c.Site.Name, err)
					}
				} else {
					sizePath := "__image_" + size.String()
					body, err = c.Serv.Monsti().GetNodeData(c.Site.Name, c.Node.Path,
						sizePath)
					if err != nil || body == nil {
						body, err = c.Serv.Monsti().GetNodeData(c.Site.Name, c.Node.Path,
							"__file_core.File")
						if err != nil {
							return fmt.Errorf("Could not get image data: %v", err)
						}
						image, err := magick.NewFromBlob(body, "jpg")
						if err != nil {
							return fmt.Errorf("Could not open image data with magick: %v", err)
						}
						defer image.Destroy()
						err = image.Resize(size.String())
						if err != nil {
							return fmt.Errorf("Could not resize image: %v", err)
						}
						body, err = image.ToBlob("jpg")
						if err != nil {
							return fmt.Errorf("Could not dump image: %v", err)
						}
						if err := c.Serv.Monsti().WriteNodeData(c.Site.Name, c.Node.Path,
							sizePath, body); err != nil {
							return fmt.Errorf("Could not write resized image data: %v", err)
						}
					}
				}
			}
			if body == nil {
				body, err = c.Serv.Monsti().GetNodeData(c.Site.Name, c.Node.Path,
					"__file_core.File")
				if err != nil {
					return fmt.Errorf("Could not read image: %v", err)
				}
			}
			c.Res.Write(body)
		} else if c.Node.Type.Id == "core.File" || c.Node.Type.Id == "core.Image" {
			content, err := c.Serv.Monsti().GetNodeData(c.Site.Name, c.Node.Path,
				"__file_core.File")
			if err != nil {
				return fmt.Errorf("Could not read file: %v", err)
			}
			c.Res.Write(content)
		} else {
			newPath, err := url.Parse(c.Node.Path + "/")
			if err != nil {
				serveError("Could not parse request URL: %v", err)
			}
			url := c.Req.URL.ResolveReference(newPath)
			http.Redirect(c.Res, c.Req, url.String(), http.StatusSeeOther)
		}
		return nil
	}

	rendered, err := h.RenderNode(c, nil)
	if err != nil {
		return fmt.Errorf("Could not render node: %v", err)
	}

	env := masterTmplEnv{Node: c.Node, Session: c.UserSession}
	var content []byte
	content = []byte(renderInMaster(h.Renderer, rendered, env, h.Settings,
		*c.Site, c.UserSession.Locale, c.Serv))

	c.Res.Write(content)
	return nil
}

func (h *nodeHandler) RenderNode(c *reqContext, embed *service.Node) (
	[]byte, error) {
	reqNode := c.Node
	if embed != nil {
		reqNode = embed
	}
	context := make(mtemplate.Context)
	context["Embed"] = make(map[string][]byte)
	// Embed nodes
	for _, embed := range reqNode.Type.Embed {
		reqURL, err := url.Parse(embed.URI)
		if err != nil {
			return nil, fmt.Errorf("Could not parse embed URI: %v", err)
		}
		embedPath := path.Join(reqNode.Path, reqURL.Path)
		node, err := c.Serv.Monsti().GetNode(c.Site.Name, embedPath)
		if err != nil || node.Type == nil {
			continue
		}
		embedNode := node
		embedNode.Path = embedPath
		rendered, err := h.RenderNode(c, embedNode)
		if err != nil {
			return nil, fmt.Errorf("Could not render embed node: %v", err)
		}
		context["Embed"].(map[string][]byte)[embed.Id] = rendered
	}
	context["Node"] = reqNode
	switch reqNode.Type.Id {
	case "core.ContactForm":
		if err := renderContactForm(c, context, h); err != nil {
			return nil, fmt.Errorf("Could not render contact form: %v", err)
		}
	case "core.NodeList":
		if err := renderNodeList(c, context, h); err != nil {
			return nil, fmt.Errorf("Could not render node list: %v", err)
		}
	}
	rendered, err := h.Renderer.Render(reqNode.Type.Id+"/view", context,
		c.UserSession.Locale, h.Settings.Monsti.GetSiteTemplatesPath(c.Site.Name))
	if err != nil {
		return nil, fmt.Errorf("Could not render template: %v", err)
	}
	return []byte(rendered), nil
}

type editFormData struct {
	NodeType string
	Name     string
	Node     service.Node
	Fields   util.NestedMap
}

// EditNode handles node edits.
func (h *nodeHandler) Edit(c *reqContext) error {
	G, _, _, _ := gettext.DefaultLocales.Use("", c.UserSession.Locale)
	h.Log.Printf("(%v) %v %v", c.Site.Name, c.Req.Method, c.Req.URL.Path)

	if err := c.Req.ParseMultipartForm(1024 * 1024); err != nil {
		if err != http.ErrNotMultipart {
			return fmt.Errorf("Could not parse form: %v", err)
		}
	}

	nodeType := c.Node.Type
	newNode := len(c.Req.FormValue("NodeType")) > 0
	if newNode {
		var err error
		nodeType, err = c.Serv.Monsti().GetNodeType(c.Req.FormValue("NodeType"))
		if err != nil {
			return fmt.Errorf("Could not get node type to add %q: %v",
				c.Req.FormValue("new"), err)
		}
		// TODO Check if node type may be added to this node
	}

	env := masterTmplEnv{Node: c.Node, Session: c.UserSession}

	if c.Action == service.EditAction {
		if newNode {
			env.Title = fmt.Sprintf(G("Add %q to \"%s\""), nodeType.Id, c.Node.Path)
		} else {
			env.Title = fmt.Sprintf(G("Edit \"%s\""), c.Node.Path)
		}
		env.Flags = EDIT_VIEW
	}

	formData := editFormData{}
	formData.Fields = make(util.NestedMap)
	if newNode {
		formData.NodeType = nodeType.Id
		formData.Node.Type = nodeType
		formData.Node.InitFields()
	} else {
		formData.Node = *c.Node
	}
	formFields := []form.Field{
		form.Field{"NodeType", "", "", nil, new(form.HiddenWidget)},
		form.Field{"Node.Hide",
			G("Hide"), G("Don't show node in navigation."), nil, nil},
		form.Field{"Node.Order",
			G("Order"), G("Order in navigation (lower numbered entries appear first)."), nil, nil},
	}
	if newNode {
		formFields = append(formFields, form.Field{"Name",
			G("Name"), G("The name as it should appear in the URL."),
			form.And(form.Required(G("Required.")), form.Regex(`^[-\w]*$`,
				G("Contains	invalid characters."))), nil})
	}

	fileFields := make([]string, 0)
	for _, field := range nodeType.Fields {
		formData.Node.GetField(field.Id).ToFormField(&formFields, formData.Fields,
			&field, c.UserSession.Locale)
		if field.Type == "File" {
			fileFields = append(fileFields, field.Id)
		}
	}

	form := form.NewForm(&formData, formFields)

	switch c.Req.Method {
	case "GET":
	case "POST":
		if len(c.Req.FormValue("New")) == 0 && form.Fill(c.Req.Form) {
			node := formData.Node
			node.Type = nodeType
			node.Path = c.Node.Path
			if newNode {
				node.InitFields()
				node.Path = path.Join(node.Path, formData.Name)
			}
			for _, field := range nodeType.Fields {
				node.GetField(field.Id).FromFormField(formData.Fields, &field)
			}
			err := c.Serv.Monsti().WriteNode(c.Site.Name, node.Path, &node)
			if err != nil {
				return fmt.Errorf("document: Could not update node: ", err)
			}

			if len(fileFields) > 0 && c.Req.MultipartForm != nil {
				for _, name := range fileFields {
					file, _, err := c.Req.FormFile("Fields." + name)
					if err == nil {
						content, err := ioutil.ReadAll(file)
						if err != nil {
							return fmt.Errorf("Could not read multipart file: %v", err)
						}
						if err = c.Serv.Monsti().WriteNodeData(c.Site.Name, node.Path,
							"__file_"+name, content); err != nil {
							return fmt.Errorf("Could not save file: %v", err)
						}
					}
				}
			}
			http.Redirect(c.Res, c.Req, node.Path+"/", http.StatusSeeOther)
			return nil
		}
	default:
		return fmt.Errorf("Request method not supported: %v", c.Req.Method)
	}
	rendered, err := h.Renderer.Render(path.Join(nodeType.Id, "edit"),
		mtemplate.Context{"Form": form.RenderData()},
		c.UserSession.Locale, h.Settings.Monsti.GetSiteTemplatesPath(c.Site.Name))

	if err != nil {
		return fmt.Errorf("Could not render template: %v", err)
	}

	content := []byte(renderInMaster(h.Renderer, []byte(rendered), env, h.Settings,
		*c.Site, c.UserSession.Locale, c.Serv))

	err = c.Session.Save(c.Req, c.Res)
	if err != nil {
		return fmt.Errorf("Could not save user session: %v", err)
	}
	c.Res.Write(content)
	return nil
}

/*
	err = c.Session.Save(c.Req, c.Res)
	if err != nil {
		return fmt.Errorf("Could not save user session: %v", err)
	}
	c.Res.Write(content)
	return nil
*/