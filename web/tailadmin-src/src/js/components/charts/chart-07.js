import ApexCharts from "apexcharts";

// ===== chartSeven
const chart07 = () => {
  const chartSevenOptions = {
    series: [45, 65, 25],
    colors: ["#3641f5", "#7592ff", "#dde9ff"],
    labels: ["Desktop", "Mobile", "Tablet"],
    chart: {
      fontFamily: "Outfit, sans-serif",
      type: "donut",
      width: 445,
      height: 290,
    },
    plotOptions: {
      pie: {
        donut: {
          size: "65%",
          background: "transparent",
          labels: {
            show: true,
            value: {
              show: true,
              offsetY: 0,
            },
          },
        },
      },
    },
    dataLabels: {
      enabled: false,
    },
    tooltip: {
      enabled: false,
    },
    stroke: {
      show: false,
      width: 4, // Creates a gap between the series
      colors: "transparent", // Gap color (use background color to make it seamless)
    },

    legend: {
      show: true,
      position: "bottom",
      horizontalAlign: "center",
      fontFamily: "Outfit",
      fontSize: "14px",
      fontWeight: 400,
      markers: {
        size: 5,
        shape: "circle",
        radius: 999,
        strokeWidth: 0,
      },
      itemMargin: {
        horizontal: 10,
        vertical: 0,
      },
    },

    responsive: [
      {
        breakpoint: 640,
        options: {
          chart: {
            width: 370,
            height: 290,
          },
        },
      },
    ],
  };

  const chartSelector = document.querySelectorAll("#chartSeven");

  if (chartSelector.length) {
    const chartSeven = new ApexCharts(
      document.querySelector("#chartSeven"),
      chartSevenOptions,
    );
    chartSeven.render();
  }
};

export default chart07;
