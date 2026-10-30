// Code generated from board.gsx by gosx compile; DO NOT EDIT.
package main

import gosx "m31labs.dev/gosx"

type ShotCardProps struct {
    Title string
    Purpose string
    Composition string
    Action string
    Camera string
}
func ShotCard(props ShotCardProps, children ...gosx.Node) gosx.Node {
    return gosx.El("article", gosx.Attrs(gosx.Attr("class", "shot-card")), gosx.El("h2", gosx.Expr(props.Title)), gosx.El("p", gosx.El("strong", gosx.Text("Purpose:")), gosx.Expr(props.Purpose)), gosx.El("p", gosx.El("strong", gosx.Text("Composition:")), gosx.Expr(props.Composition)), gosx.El("p", gosx.El("strong", gosx.Text("Action:")), gosx.Expr(props.Action)), gosx.El("p", gosx.El("strong", gosx.Text("Camera:")), gosx.Expr(props.Camera)))
}
