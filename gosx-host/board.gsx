package main

type ShotCardProps struct {
    Title string
    Purpose string
    Composition string
    Action string
    Camera string
}

component ShotCard(props: ShotCardProps) {
    return <article class="shot-card">
        <h2>{props.Title}</h2>
        <p><strong>Purpose:</strong> {props.Purpose}</p>
        <p><strong>Composition:</strong> {props.Composition}</p>
        <p><strong>Action:</strong> {props.Action}</p>
        <p><strong>Camera:</strong> {props.Camera}</p>
    </article>
}
